package productadapter

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func ProductSSHConfig(authority Authority, consumer ConsumerManifest, count, index int) ([]byte, error) {
	parsed, err := url.Parse(authority.Adapter.ProductOrigin)
	if err != nil || parsed.User == nil || parsed.User.Username() == "" {
		return nil, fmt.Errorf("Product origin has no exact SSH user")
	}
	if count != authority.Adapter.Count || index != consumer.Index {
		return nil, fmt.Errorf("Product SSH config identity does not match authority")
	}
	return []byte(strings.Join([]string{
		"Host github.com ssh.github.com",
		"  HostName ssh.github.com",
		"  Port 443",
		"  User " + parsed.User.Username(),
		"  IdentitiesOnly yes",
		"  IdentityFile " + consumer.SSHIdentityPath,
		"  UserKnownHostsFile " + filepath.Join(consumer.HomePath, ".ssh", "known_hosts"),
		"  StrictHostKeyChecking yes",
		"  ProxyCommand " + authority.Adapter.ProxyHelperPath +
			" stdio --count " + strconv.Itoa(count) +
			" --index " + strconv.Itoa(index),
		"",
	}, "\n")), nil
}
