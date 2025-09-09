package netutil

import (
    "encoding/json"
    "net"
    "os"
    "os/exec"
    "strings"
)

type routeEntry struct {
    Dst string `json:"dst"`
}

// PickInternalSubnet returns a non-overlapping /24 CIDR and a DNS IP (.3) for the internal network.
// Honors DEVKIT_INTERNAL_SUBNET and DEVKIT_DNS_IP if set. Falls back to defaults if detection fails.
func PickInternalSubnet() (cidr string, dnsIP string) {
    // Respect explicit overrides
    if v := strings.TrimSpace(os.Getenv("DEVKIT_INTERNAL_SUBNET")); v != "" {
        cidr = v
        if d := strings.TrimSpace(os.Getenv("DEVKIT_DNS_IP")); d != "" {
            dnsIP = d
        } else {
            dnsIP = dnsFromCIDR(cidr)
        }
        return
    }

    // Default candidates within 172.30.0.0/16
    candidates := []string{
        "172.30.10.0/24",
        "172.30.20.0/24",
        "172.30.30.0/24",
        "172.30.40.0/24",
        "172.30.50.0/24",
        "172.30.60.0/24",
        "172.30.70.0/24",
        "172.30.80.0/24",
        "172.30.90.0/24",
        "172.30.100.0/24",
    }

    used := getUsedCIDRs()
    for _, c := range candidates {
        if !overlapsAny(c, used) {
            cidr = c
            dnsIP = dnsFromCIDR(cidr)
            return
        }
    }
    // Fallback to default
    cidr = "172.30.10.0/24"
    dnsIP = dnsFromCIDR(cidr)
    return
}

func dnsFromCIDR(cidr string) string {
    ip, ipnet, err := net.ParseCIDR(cidr)
    if err != nil { return "172.30.10.3" }
    ip4 := ip.To4()
    if ip4 == nil { return ip.String() }
    base := make(net.IP, len(ip4))
    copy(base, ip4)
    // add 3 to network address
    base[3] += 3
    if !ipnet.Contains(base) {
        // fallback to .3 in common case
        parts := strings.Split(ipnet.IP.String(), ".")
        if len(parts) == 4 { parts[3] = "3"; return strings.Join(parts, ".") }
        return ipnet.IP.String()
    }
    return base.String()
}

func overlapsAny(candidate string, used []string) bool {
    _, cn, err := net.ParseCIDR(candidate)
    if err != nil { return true }
    for _, u := range used {
        _, un, err := net.ParseCIDR(u)
        if err != nil { continue }
        if cidrOverlap(cn, un) { return true }
    }
    return false
}

func cidrOverlap(a, b *net.IPNet) bool {
    return a.Contains(b.IP) || b.Contains(a.IP)
}

func getUsedCIDRs() []string {
    // Try `ip -j route` first
    out, err := exec.Command("ip", "-j", "route").Output()
    if err == nil {
        var entries []map[string]any
        if json.Unmarshal(out, &entries) == nil {
            var res []string
            for _, e := range entries {
                if dst, ok := e["dst"].(string); ok {
                    if dst == "default" || !strings.Contains(dst, "/") { continue }
                    res = append(res, dst)
                }
            }
            if len(res) > 0 { return res }
        }
    }
    // Fallback: `ip route`
    out, err = exec.Command("ip", "route").Output()
    if err != nil { return nil }
    var res []string
    for _, line := range strings.Split(string(out), "\n") {
        fields := strings.Fields(line)
        if len(fields) == 0 { continue }
        if fields[0] == "default" { continue }
        if strings.Contains(fields[0], "/") {
            res = append(res, fields[0])
        }
    }
    return res
}

