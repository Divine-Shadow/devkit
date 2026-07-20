//go:build !devkitintegration

package productadapter

func authorityPermitsProductOrigin(origin string) bool {
	return isProductSSHOrigin(origin)
}
