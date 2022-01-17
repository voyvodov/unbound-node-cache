package netif

import (
	"fmt"
	"net"
)

func ParseMAC(prefix net.IP, mac net.HardwareAddr) (net.IP, error) {
	if !isIPv6Addr(prefix) {
		return nil, fmt.Errorf("IP must be an IPv6 address")
	}

	// Prefix must be 64 bits or less in length, meaning the last 8
	// bytes must be entirely zero.
	if !isAllZeroes(prefix[8:16]) {
		return nil, fmt.Errorf("prefix must be an IPv6 address prefix of /64 or less")
	}

	// MAC must be in EUI-48 or EUI64 form.
	if len(mac) != 6 && len(mac) != 8 {
		return nil, fmt.Errorf("MAC address must be in EUI-48 or EUI-64 form")
	}

	// Copy prefix directly into first 8 bytes of IP address.
	ip := make(net.IP, 16)
	copy(ip[0:8], prefix[0:8])

	// Flip 7th bit from left on the first byte of the MAC address, the
	// "universal/local (U/L)" bit.  See RFC 4291, Section 2.5.1 for more
	// information.

	// If MAC is in EUI-64 form, directly copy it into output IP address.
	if len(mac) == 8 {
		copy(ip[8:16], mac)
		ip[8] ^= 0x02
		return ip, nil
	}

	// If MAC is in EUI-48 form, split first three bytes and last three bytes,
	// and inject 0xff and 0xfe between them.
	copy(ip[8:11], mac[0:3])
	ip[8] ^= 0x02
	ip[11] = 0xff
	ip[12] = 0xfe
	copy(ip[13:16], mac[3:6])

	return ip, nil
}

// isAllZeroes returns if a byte slice is entirely populated with byte 0.
func isAllZeroes(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != 0 {
			return false
		}
	}

	return true
}

// isIPv6Addr returns if an IP address is a valid IPv6 address.
func isIPv6Addr(ip net.IP) bool {
	if ip.To16() == nil {
		return false
	}

	return ip.To4() == nil
}
