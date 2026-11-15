terraform {
  required_providers {
    allot = {
      source = "byteshuttle/allot"
    }
  }
}

# Uses ALLOT_STORE and standard AWS SDK credential sources.
provider "allot" {}

# Logical keys are unique across a space, independent of parent relationships.

resource "allot_space" "ipv4" {
  key  = "home/ipv4"
  kind = "ipv4"
  cidr = "10.0.0.0/8"
}

resource "allot_space" "ipv6" {
  key  = "home/ula"
  kind = "ipv6"
  cidr = "fd12:3456:789a::/48"
}

resource "allot_claim" "lan_v4" {
  within = allot_space.ipv4.ref
  key    = "lan"
  cidr   = "10.0.0.0/24"
}

resource "allot_claim" "lan_v6" {
  within = allot_space.ipv6.ref
  key    = "lan"
  cidr   = "fd12:3456:789a::/64"
}

resource "allot_claim" "gateway" {
  within = allot_claim.lan_v4.ref
  key    = "lan/gateway"
  offset = 1
}

resource "allot_claim" "dhcp" {
  within = allot_claim.lan_v4.ref
  key    = "lan/dhcp"

  range = {
    start_offset = 2
    end_offset   = -2
  }
}

resource "allot_claim" "printer" {
  within = allot_claim.dhcp.ref
  key    = "lan/printer"
  value  = "10.0.0.150"
}

resource "allot_claim" "future_network" {
  within = allot_space.ipv4.ref
  key    = "future-network"
  cidr   = "10.0.1.0/24"
}

# A downstream DHCP module would consume the interval and subtract fixed child
# addresses before rendering dynamic ranges. A binding is separate delivery
# metadata, not an allocator record. No live lease coordination is implemented.
output "dhcp_intent" {
  value = {
    interval           = { start = allot_claim.dhcp.result.start, end = allot_claim.dhcp.result.end }
    excluded_addresses = [allot_claim.printer.result.value]
    fixed_bindings = {
      printer = {
        address = allot_claim.printer.result.value
        mac     = "02:00:00:00:00:01"
      }
    }
  }
}
