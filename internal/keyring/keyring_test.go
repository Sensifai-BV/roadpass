package keyring

import (
	"regexp"
	"testing"
)

// OpenBao's route pattern for Transit key names (framework.GenericNameRegex).
var transitName = regexp.MustCompile(`^\w(([\w-.]+)?\w)?$`)

func TestVaultNamesAreValidForEveryID(t *testing.T) {
	for _, id := range []string{"usr_abc-", "usr_-x-", "org_a.b", "usr__", "usr_a/b", "org_ünï"} {
		for _, n := range []string{vaultName("", id), vaultName(id, "")} {
			if !transitName.MatchString(n) {
				t.Errorf("%q → %q is not a valid Transit key name", id, n)
			}
		}
	}
	if vaultName("org_a", "") == vaultName("", "org_a") {
		t.Error("company and personal key names must not collide")
	}
}
