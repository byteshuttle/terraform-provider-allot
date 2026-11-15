terraform {
  required_providers {
    allot = {
      source = "byteshuttle/allot"
    }
  }
}

# Uses ALLOT_STORE and standard AWS SDK credential sources.
provider "allot" {}


resource "allot_space" "vlan" {
  key  = "fabric/home/vlan"
  kind = "integer"

  range = {
    start = 1
    end   = 4094
  }
}

# A /56 delegation contains 256 /64 selectors. Scope follows the logical
# delegation rather than its current, potentially changing public prefix.
resource "allot_space" "sla" {
  key  = "isp/primary/pd0/sla"
  kind = "integer"

  range = {
    start = 0
    end   = 255
  }
}

resource "allot_claim" "lan_vlan" {
  within = allot_space.vlan.ref
  key    = "lan"
  value  = 101
}

resource "allot_claim" "lan_sla" {
  within = allot_space.sla.ref
  key    = "lan"
  value  = 0
}

resource "allot_claim" "lab_vlans" {
  within = allot_space.vlan.ref
  key    = "lab"

  range = {
    start = 200
    end   = 299
  }
}

resource "allot_claim" "lab_network" {
  within = allot_claim.lab_vlans.ref
  key    = "lab/network"

  request = {
    kind = "value"
  }
}
