package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/byteshuttle/allot"
	"github.com/byteshuttle/allot/backend"
	"github.com/byteshuttle/terraform-provider-allot/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var buildOnce sync.Once
var providerDir, buildError string

func testCLI() string {
	if path := os.Getenv("TF_ACC_TERRAFORM_PATH"); path != "" {
		return path
	}
	return "terraform"
}

func testHost() string {
	if host := os.Getenv("TF_ACC_PROVIDER_HOST"); host != "" {
		return host
	}
	return "registry.terraform.io"
}

func buildProvider(t *testing.T) string {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run real Terraform lifecycle checks")
	}
	if _, err := exec.LookPath(testCLI()); err != nil {
		t.Fatal("Terraform is required for acceptance tests")
	}
	buildOnce.Do(func() {
		var err error
		providerDir, err = os.MkdirTemp("", "allot-provider-test-*")
		if err != nil {
			buildError = err.Error()
			return
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(providerDir, "terraform-provider-allot"), ".")
		cmd.Dir = "../.."
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildError = fmt.Sprintf("%v: %s", err, out)
		}
	})
	if buildError != "" {
		t.Fatal(buildError)
	}
	return providerDir
}

type harness struct {
	t          *testing.T
	dir, store string
	env        []string
}

func newHarness(t *testing.T, store string, extra string) *harness {
	t.Helper()
	bin := buildProvider(t)
	dir := t.TempDir()
	cli := fmt.Sprintf("provider_installation {\n dev_overrides {\n \"byteshuttle/allot\" = %s\n }\n direct {}\n}\n", strconv.Quote(bin))
	cli = strings.ReplaceAll(cli, "byteshuttle/allot", testHost()+"/byteshuttle/allot")
	cliPath := filepath.Join(dir, "terraform.rc")
	if err := os.WriteFile(cliPath, []byte(cli), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TF_") && !strings.HasPrefix(entry, "TOFU_") {
			env = append(env, entry)
		}
	}
	h := &harness{t: t, dir: dir, store: store, env: append(env, "TF_CLI_CONFIG_FILE="+cliPath, "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1")}
	h.write(fmt.Sprintf("provider \"allot\" {\n store = %s\n %s\n}\n", strconv.Quote(store), extra))
	return h
}
func (h *harness) write(config string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, "main.tf"), []byte(fmt.Sprintf("terraform {\n required_providers {\n allot = { source = %q }\n }\n}\n", testHost()+"/byteshuttle/allot")+config), 0600); err != nil {
		h.t.Fatal(err)
	}
}
func (h *harness) tf(want int, args ...string) string {
	h.t.Helper()
	commandArgs := append([]string{args[0], "-no-color"}, args[1:]...)
	if args[0] == "state" {
		commandArgs = append([]string{args[0], args[1], "-no-color"}, args[2:]...)
	}
	cmd := exec.Command(testCLI(), commandArgs...)
	cmd.Dir = h.dir
	cmd.Env = h.env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			h.t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	if code != want {
		h.t.Fatalf("terraform %v: exit=%d want=%d\n%s", args, code, want, out)
	}
	return string(out)
}
func (h *harness) values() map[string]map[string]any {
	h.t.Helper()
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string         `json:"address"`
					Values  map[string]any `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	// Terraform show JSON cannot be mixed with a command warning on stdout.
	cmd := exec.Command(testCLI(), "show", "-json")
	cmd.Dir = h.dir
	cmd.Env = h.env
	out, err := cmd.Output()
	if err != nil {
		h.t.Fatal(err)
	}
	if err := json.Unmarshal(out, &state); err != nil {
		h.t.Fatalf("state JSON: %v: %s", err, out)
	}
	result := map[string]map[string]any{}
	for _, r := range state.Values.Root.Resources {
		result[r.Address] = r.Values
	}
	return result
}
func lifecycleConfig(store, extra string, value int) string {
	return fmt.Sprintf(`
provider "allot" {
 store = %q
%s
}
resource "allot_space" "home" {
 key = "home/ipv4"
  cidr = "10.0.0.0/24"
}
resource "allot_space" "ula" {
 key = "home/ipv6"
  cidr = "fd12::/48"
}
resource "allot_space" "ids" {
 key = "fabric/vlan"
  kind = "integer"
  range = { start = 1, end = 4094 }
}
resource "allot_claim" "dhcp" {
 key = "dhcp"
  within = allot_space.home.ref
  range = { start_offset = 2, end_offset = -2 }
}
resource "allot_claim" "printer" {
 key = "printer"
  within = allot_claim.dhcp.ref
  value = "10.0.0.150"
}
resource "allot_claim" "vlan" {
 key = "lan"
  within = allot_space.ids.ref
  value = %d
}
resource "allot_claim" "automatic" {
 key = "automatic"
  within = allot_space.ids.ref
}
resource "allot_claim" "lan6" {
 key = "lan"
  within = allot_space.ula.ref
  request = { prefix_length = 64 }
}
resource "allot_claim" "fixed6" {
  key = "fixed"
  within = allot_space.ula.ref
  cidr = "fd12:0:0:1::/64"
}
resource "allot_claim" "gateway" {
  key = "gateway"
  within = allot_space.home.ref
  offset = 1
}
resource "allot_claim" "reserved" {
  key = "reserved"
  within = allot_space.ids.ref
  range = {start=200,end=209}
}
resource "allot_claim" "batch" {
  key = "batch"
  within = allot_space.ids.ref
  request = {count=3}
}
resource "allot_claim" "explicit_auto" {
  key = "explicit-auto"
  within = allot_space.ids.ref
  request = {kind="value"}
}
`, store, extra, value)
}

// The framework harness verifies refresh consistency and empty post-apply plans.
// Separate CLI tests below exercise failed operations and external ownership.
func runLifecycle(t *testing.T, config backend.Config, extra string) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
	store := config.Store
	s, err := backend.Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	client, err := allot.NewClient(s, allot.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var oldID string
	steps := []resource.TestStep{{
		Config: lifecycleConfig(store, extra, 101),
		Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("allot_claim.printer", "result.value", "10.0.0.150"),
			resource.TestCheckResourceAttr("allot_claim.lan6", "result.cidr", "fd12::/64"),
			func(state *terraform.State) error {
				oldID = state.RootModule().Resources["allot_claim.vlan"].Primary.ID
				return nil
			},
		),
	}}
	for _, item := range []struct{ address, key string }{
		{"allot_claim.printer", "home/ipv4"}, {"allot_claim.dhcp", "home/ipv4"},
		{"allot_claim.vlan", "fabric/vlan"}, {"allot_claim.automatic", "fabric/vlan"},
		{"allot_claim.lan6", "home/ipv6"}, {"allot_claim.fixed6", "home/ipv6"},
		{"allot_claim.gateway", "home/ipv4"}, {"allot_claim.reserved", "fabric/vlan"},
		{"allot_claim.batch", "fabric/vlan"}, {"allot_claim.explicit_auto", "fabric/vlan"},
		{"allot_space.home", "home/ipv4"}, {"allot_space.ula", "home/ipv6"}, {"allot_space.ids", "fabric/vlan"},
	} {
		steps = append(steps, resource.TestStep{ResourceName: item.address, ImportState: true, ImportStateIdPrefix: item.key + "/", ImportStateVerify: true})
	}
	steps = append(steps, resource.TestStep{Config: lifecycleConfig(store, extra, 102), Check: resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr("allot_claim.vlan", "result.value", "102"),
		func(state *terraform.State) error {
			if state.RootModule().Resources["allot_claim.vlan"].Primary.ID == oldID {
				return fmt.Errorf("replacement reused allocation UUID")
			}
			return nil
		},
	)})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"allot": providerserver.NewProtocol6WithError(provider.New("test")())},
		Steps:                    steps,
		CheckDestroy: func(_ *terraform.State) error {
			spaces, err := client.ListSpaces(context.Background())
			if err != nil {
				return err
			}
			for _, space := range spaces {
				if !space.Released {
					return fmt.Errorf("destroy left active space: %v", space)
				}
			}
			return nil
		},
	})
}
func TestAccLocalLifecycle(t *testing.T) { runLifecycle(t, backend.Config{Store: t.TempDir()}, "") }

// Planning must leave the allocation ledger empty.
func TestAccPlanDoesNotAllocate(t *testing.T) {
	buildProvider(t)
	store := t.TempDir()
	h := newHarness(t, store, "")
	h.write(lifecycleConfig(store, "", 101))
	h.tf(2, "plan", "-detailed-exitcode")
	s, err := backend.Open(context.Background(), backend.Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	c, err := allot.NewClient(s, allot.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spaces, err := c.ListSpaces(context.Background())
	if err != nil || len(spaces) != 0 {
		t.Fatalf("planning allocated capacity: %v, %v", spaces, err)
	}
}
func TestAccS3Lifecycle(t *testing.T) {
	endpoint, bucket := os.Getenv("ALLOT_TEST_S3_ENDPOINT"), os.Getenv("ALLOT_TEST_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("dedicated S3 test endpoint/bucket required")
	}
	region := os.Getenv("ALLOT_TEST_S3_REGION")
	if region == "" {
		region = "us-east-1"
	}
	runLifecycle(t, backend.Config{Store: "s3://" + bucket + "/provider-test-" + allot.NewID(), Endpoint: endpoint, Region: region, UsePathStyle: true}, fmt.Sprintf("endpoint = %q\nregion = %q\npath_style = true", endpoint, region))
}

// Destroy must retain state when a consumer outside this Terraform state still
// owns a child, then succeed after that child is explicitly released.
func TestAccDestroyNonemptyRoot(t *testing.T) {
	buildProvider(t)
	store := t.TempDir()
	h := newHarness(t, store, "")
	config := fmt.Sprintf("provider \"allot\" { store = %q }\nresource \"allot_space\" \"home\" {\nkey=\"home\"\nkind=\"integer\"\nrange={start=0,end=9}\n}\n", store)
	h.write(config)
	h.tf(0, "apply", "-auto-approve")
	id := h.values()["allot_space.home"]["id"].(string)
	s, err := backend.Open(context.Background(), backend.Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	c, err := allot.NewClient(s, allot.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ref := allot.SpaceRef{Key: "home", ID: id}
	child, err := c.Allocate(context.Background(), ref, id, allot.Request{ID: allot.NewID(), Key: "external", Select: allot.Selection{Value: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	output := h.tf(1, "destroy", "-auto-approve")
	if !strings.Contains(strings.Join(strings.Fields(output), " "), "allocation has children") {
		t.Fatalf("destroy failed for another reason: %s", output)
	}
	if h.values()["allot_space.home"]["id"] != id {
		t.Fatal("failed destroy lost root identity")
	}
	if err := c.Release(context.Background(), ref, child.ID); err != nil {
		t.Fatal(err)
	}
	h.tf(0, "destroy", "-auto-approve")
}

func TestAccRootReplacement(t *testing.T) {
	buildProvider(t)
	store := t.TempDir()
	h := newHarness(t, store, "")
	config := func(end int) string {
		return fmt.Sprintf(`
provider "allot" { store = %q }
resource "allot_space" "ids" {
  key = "ids"
  kind = "integer"
  range = {start=0,end=%d}
}
resource "allot_claim" "one" {
  key = "one"
  within = allot_space.ids.ref
}
`, store, end)
	}
	h.write(config(9))
	h.tf(0, "apply", "-auto-approve")
	before := h.values()
	h.write(config(19))
	h.tf(0, "apply", "-auto-approve")
	h.tf(0, "plan", "-detailed-exitcode")
	after := h.values()
	for _, address := range []string{"allot_space.ids", "allot_claim.one"} {
		if before[address]["id"] == after[address]["id"] {
			t.Fatalf("replacement retained UUID: %s", address)
		}
	}
	if after["allot_space.ids"]["result"].(map[string]any)["end"] != "19" {
		t.Fatal("root replacement retained old bounds")
	}
	h.tf(0, "destroy", "-auto-approve")
}

func TestAccExplicitAdoption(t *testing.T) {
	buildProvider(t)
	store := t.TempDir()
	s, err := backend.Open(context.Background(), backend.Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	c, err := allot.NewClient(s, allot.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	space := allot.Space{ID: allot.NewID(), Key: "external", Kind: allot.IPv4, Range: allot.Range{Start: "10.0.0.0", End: "10.0.0.255"}}
	if _, err := c.Create(context.Background(), space); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, store, "")
	h.write(fmt.Sprintf("provider \"allot\" {store=%q}\nresource \"allot_space\" \"external\" {\nkey=\"external\"\ncidr=\"10.0.0.0/24\"\n}\n", store))
	output := h.tf(1, "apply", "-auto-approve")
	if !strings.Contains(strings.Join(strings.Fields(output), " "), "different identity") {
		t.Fatalf("apply failed for another reason: %s", output)
	}
	if _, managed := h.values()["allot_space.external"]; managed {
		t.Fatal("same-name root silently adopted")
	}
	h.tf(0, "import", "allot_space.external", "external/"+space.ID)
	h.tf(0, "plan", "-detailed-exitcode")
	h.tf(0, "destroy", "-auto-approve")
}

func TestAccRequestCompatibility(t *testing.T) {
	buildProvider(t)
	store := t.TempDir()
	h := newHarness(t, store, "")
	config := func(selector string) string {
		return fmt.Sprintf(`
provider "allot" {store=%q}
resource "allot_space" "ids" {
  key="ids"
  kind="integer"
  range={start=0,end=9}
}
resource "allot_claim" "one" {
  key="one"
  within=allot_space.ids.ref
  %s
}
`, store, selector)
	}
	var previous any
	for _, selector := range []string{"request={count=3}", "request={}", "value=7", ""} {
		h.write(config(selector))
		h.tf(0, "apply", "-auto-approve")
		h.tf(0, "plan", "-detailed-exitcode")
		claim := h.values()["allot_claim.one"]
		if previous != nil {
			// Relaxing an exact singleton to automatic keeps the allocation;
			// incompatible capacity and changes to an exact selector replace it.
			if selector == "" && claim["id"] != previous {
				t.Fatal("compatible singleton request replaced allocation")
			}
			if selector != "" && claim["id"] == previous {
				t.Fatal("incompatible selector retained allocation")
			}
		}
		previous = claim["id"]
		if selector != "request={count=3}" && claim["result"].(map[string]any)["value"] == nil {
			t.Fatal("singleton request retained a range")
		}
	}
	h.tf(0, "destroy", "-auto-approve")
}

// A capacity mismatch must plan replacement, without modifying the ledger.
func (h *harness) requireReplacement(address string) {
	h.t.Helper()
	h.tf(2, "plan", "-detailed-exitcode", "-out=change.tfplan")
	cmd := exec.Command(testCLI(), "show", "-json", "change.tfplan")
	cmd.Dir, cmd.Env = h.dir, h.env
	out, err := cmd.Output()
	if err != nil {
		h.t.Fatal(err)
	}
	var plan struct {
		Changes []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err = json.Unmarshal(out, &plan); err != nil {
		h.t.Fatal(err)
	}
	for _, change := range plan.Changes {
		if change.Address == address && strings.Join(change.Change.Actions, ",") == "delete,create" {
			return
		}
	}
	h.t.Fatalf("%s must require replacement: %s", address, out)
}

// Import adopts the allocation UUID, while an automatic request constrains its
// existing capacity rather than requiring the original allocation selector.
func TestAccImportExactClaimAsAutomatic(t *testing.T) {
	buildProvider(t)
	for _, tc := range []struct {
		name      string
		kind      allot.Kind
		bounds    allot.Range
		selection allot.Selection
		request   string
		fits      bool
	}{
		{"ipv4_prefix", allot.IPv4, allot.Range{Start: "10.0.0.0", End: "10.0.255.255"}, allot.Selection{Prefix: "10.0.4.0/24"}, "request={prefix_length=24}", true},
		{"ipv6_prefix", allot.IPv6, allot.Range{Start: "fd12::", End: "fd12::ffff:ffff:ffff:ffff:ffff"}, allot.Selection{Prefix: "fd12:0:0:abcd::/64"}, "request={prefix_length=64}", true},
		{"integer_count", allot.Integer, allot.Range{Start: "0", End: "1000"}, allot.Selection{Range: &allot.Range{Start: "110", End: "119"}}, "request={count=10}", true},
		{"ipv4_count", allot.IPv4, allot.Range{Start: "10.0.0.0", End: "10.0.255.255"}, allot.Selection{Range: &allot.Range{Start: "10.0.4.3", End: "10.0.4.12"}}, "request={count=10}", true},
		{"ipv6_large_count", allot.IPv6, allot.Range{Start: "fd12::", End: "fd12::ffff:ffff:ffff:ffff:ffff"}, allot.Selection{Prefix: "fd12:0:0:abcd::/64"}, "request={count=\"18446744073709551616\"}", true},
		{"implicit_singleton", allot.Integer, allot.Range{Start: "0", End: "1000"}, allot.Selection{Value: "101"}, "", true},
		{"explicit_singleton", allot.IPv4, allot.Range{Start: "10.0.0.0", End: "10.0.255.255"}, allot.Selection{Value: "10.0.4.7"}, "request={}", true},
		{"prefix_too_large", allot.IPv4, allot.Range{Start: "10.0.0.0", End: "10.0.255.255"}, allot.Selection{Prefix: "10.0.4.0/23"}, "request={prefix_length=24}", false},
		{"prefix_too_small", allot.IPv4, allot.Range{Start: "10.0.0.0", End: "10.0.255.255"}, allot.Selection{Prefix: "10.0.4.0/25"}, "request={prefix_length=24}", false},
		{"unaligned_prefix", allot.IPv4, allot.Range{Start: "10.0.0.0", End: "10.0.255.255"}, allot.Selection{Range: &allot.Range{Start: "10.0.4.1", End: "10.0.5.0"}}, "request={prefix_length=24}", false},
		{"count_mismatch", allot.Integer, allot.Range{Start: "0", End: "1000"}, allot.Selection{Range: &allot.Range{Start: "110", End: "120"}}, "request={count=10}", false},
		{"range_is_not_singleton", allot.Integer, allot.Range{Start: "0", End: "1000"}, allot.Selection{Range: &allot.Range{Start: "110", End: "119"}}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := t.TempDir()
			s, err := backend.Open(ctx, backend.Config{Store: store})
			if err != nil {
				t.Fatal(err)
			}
			c, err := allot.NewClient(s, allot.ClientOptions{})
			if err != nil {
				t.Fatal(err)
			}
			space := allot.Space{ID: allot.NewID(), Key: "external", Kind: tc.kind, Range: tc.bounds}
			if _, err = c.Create(ctx, space); err != nil {
				t.Fatal(err)
			}
			claim, err := c.Allocate(ctx, space.Ref(), space.ID, allot.Request{ID: allot.NewID(), Key: "existing", Select: tc.selection})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "ipv4_prefix" {
				if _, err := c.Allocate(ctx, space.Ref(), claim.ID, allot.Request{ID: allot.NewID(), Key: "consumer", Select: allot.Selection{Value: "10.0.4.7"}}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := c.Load(ctx, space.Ref())
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, err := before.Encode()
			if err != nil {
				t.Fatal(err)
			}
			h := newHarness(t, store, "")
			config := fmt.Sprintf(`
provider "allot" {store=%q}
resource "allot_space" "external" {
  key="external"
  kind=%q
  range={start=%q,end=%q}
}
resource "allot_claim" "existing" {
  key="existing"
  within=allot_space.external.ref
  %s
}
`, store, tc.kind, tc.bounds.Start, tc.bounds.End, tc.request)
			h.write(config)
			h.tf(0, "import", "allot_space.external", "external/"+space.ID)
			h.tf(0, "import", "allot_claim.existing", "external/"+claim.ID)
			if tc.fits {
				// Applying may update Terraform's selector, but must keep the
				// allocation UUID and leave the allocation store untouched.
				h.tf(0, "apply", "-auto-approve")
				h.tf(0, "plan", "-detailed-exitcode")
				h.tf(0, "apply", "-refresh-only", "-auto-approve")
				h.tf(0, "plan", "-detailed-exitcode")
				if h.values()["allot_claim.existing"]["id"] != claim.ID {
					t.Fatal("compatible import replaced the allocation UUID")
				}
				if tc.name == "ipv4_prefix" {
					h.write(strings.Replace(config, `key="existing"`, `key="renamed"`, 1))
					h.requireReplacement("allot_claim.existing")
					other := fmt.Sprintf(`
resource "allot_space" "other" {
  key="other"
  cidr="10.0.0.0/16"
}
`)
					h.write(strings.Replace(config, "within=allot_space.external.ref", "within=allot_space.other.ref", 1) + other)
					h.requireReplacement("allot_claim.existing")
				}
			} else {
				h.requireReplacement("allot_claim.existing")
			}
			after, err := c.Load(ctx, space.Ref())
			if err != nil {
				t.Fatal(err)
			}
			afterJSON, err := after.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if string(beforeJSON) != string(afterJSON) {
				t.Fatal("reconciling imported selectors mutated the allocation store")
			}
		})
	}
}
