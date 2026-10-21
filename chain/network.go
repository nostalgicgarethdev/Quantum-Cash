package chain

import "fmt"

// Network is a named QuantumCash network. Each network has its own genesis
// seed, so devnet and testnet nodes refuse to talk to each other.
type Network struct {
	Name     string
	Genesis  string // genesis seed hashed into GenesisPoH
	Bits     uint8  // default PoW difficulty
	P2PPort  int
	HTTPPort int
	Seeds    []string // default bootstrap peers (host:port). Empty is fine.
}

// Networks known to this build. Add public seed hostnames to Testnet.Seeds
// once seed servers are online (e.g. "seed1.quantumcash.network:7001").
var Networks = map[string]Network{
	"devnet": {Name: "devnet", Genesis: "QuantumCash genesis — experimental testnet, not money",
		Bits: DefaultBits, P2PPort: 7001, HTTPPort: 8545},
	"testnet": {Name: "testnet", Genesis: "QuantumCash public testnet-1 genesis — 2026-10-21 — not money",
		Bits: 18, P2PPort: 7101, HTTPPort: 8645, Seeds: []string{}},
}

// ActiveNetwork is the network selected by UseNetwork (devnet by default).
var ActiveNetwork = Networks["devnet"]

// UseNetwork switches the process to a named network. Call before New.
func UseNetwork(name string) (Network, error) {
	n, ok := Networks[name]
	if !ok {
		return Network{}, fmt.Errorf("unknown network %q (devnet, testnet)", name)
	}
	ActiveNetwork = n
	GenesisPoH = sha([]byte(n.Genesis))
	return n, nil
}
