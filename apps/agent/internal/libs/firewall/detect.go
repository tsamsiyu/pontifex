package firewall

import "os"

// Detect returns an iptables- or nftables-backed Firewall. With BackendAuto,
// it picks nftables if /proc/net/nf_tables exists, else iptables.
func Detect(backend Backend) Firewall {
	switch backend {
	case BackendIPTables:
		return NewIPTables()
	case BackendNFTables:
		return NewNFTables()
	case BackendAuto:
		fallthrough
	default:
		if _, err := os.Stat("/proc/net/nf_tables"); err == nil {
			return NewNFTables()
		}
		return NewIPTables()
	}
}
