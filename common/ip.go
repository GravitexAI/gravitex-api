package common

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func IsIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil
}

func ParseIP(s string) net.IP {
	return net.ParseIP(s)
}

func IsPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	private := []net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	}

	for _, privateNet := range private {
		if privateNet.Contains(ip) {
			return true
		}
	}
	return false
}

func IsIpInCIDRList(ip net.IP, cidrList []string) bool {
	for _, cidr := range cidrList {
		if strings.Contains(cidr, "*") {
			if matchesIPv4Wildcard(ip, cidr) {
				return true
			}
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// 尝试作为单个IP处理
			if whitelistIP := net.ParseIP(cidr); whitelistIP != nil {
				if ip.Equal(whitelistIP) {
					return true
				}
			}
			continue
		}

		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateIPAllowlist accepts one IP, CIDR, or trailing-segment IPv4 wildcard per line.
func ValidateIPAllowlist(allowlist string) error {
	for _, line := range strings.Split(allowlist, "\n") {
		rule := strings.TrimSpace(line)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "*") {
			if err := validateIPv4Wildcard(rule); err != nil {
				return fmt.Errorf("invalid IP allowlist entry %q: %w", rule, err)
			}
			continue
		}
		if net.ParseIP(rule) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(rule); err != nil {
			return fmt.Errorf("invalid IP allowlist entry %q: expected an IP address, CIDR, or trailing-segment IPv4 wildcard", rule)
		}
	}
	return nil
}

func validateIPv4Wildcard(rule string) error {
	parts := strings.Split(rule, ".")
	if len(parts) < 2 || len(parts) > 4 || parts[len(parts)-1] != "*" {
		return fmt.Errorf("wildcards are only supported as the final IPv4 segment")
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return fmt.Errorf("wildcard entries must contain valid IPv4 segments")
		}
		value, err := strconv.Atoi(part)
		if err != nil || value > 255 {
			return fmt.Errorf("wildcard entries must contain valid IPv4 segments")
		}
	}
	return nil
}

func matchesIPv4Wildcard(ip net.IP, rule string) bool {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return false
	}
	parts := strings.Split(rule, ".")
	if len(parts) < 2 || len(parts) > 4 || parts[len(parts)-1] != "*" {
		return false
	}
	for i, part := range parts[:len(parts)-1] {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 || int(ipv4[i]) != value {
			return false
		}
	}
	return true
}
