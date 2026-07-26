package authenv

import (
	"slices"
	"testing"
)

// The parsing rules here are Django's, in config/settings.py (_admin_emails,
// _bool_env). A deployment sets one ADMIN_EMAILS and one password-login switch
// for both applications during the dual-stack rollout, so the two have to read
// them the same way.

func TestLoadAdminsNormalisesTheList(t *testing.T) {
	t.Setenv(EnvAdminEmails, " Chief@GolfTrack.TEST ,, deputy@golftrack.test,")

	admins := LoadAdmins()

	if !admins.Enforced() {
		t.Fatal("a non-empty list is not enforced")
	}
	if got := len(admins); got != 2 {
		t.Errorf("parsed %d addresses, want 2 (blanks between commas should be dropped)", got)
	}
	for _, email := range []string{
		"chief@golftrack.test",
		"CHIEF@GOLFTRACK.TEST",
		"  chief@golftrack.test  ",
		"deputy@golftrack.test",
	} {
		if !admins.Contains(email) {
			t.Errorf("Contains(%q) = false, want true", email)
		}
	}
	for _, email := range []string{"", "   ", "someone@golftrack.test"} {
		if admins.Contains(email) {
			t.Errorf("Contains(%q) = true, want false", email)
		}
	}
}

// An unset or blank list is a no-op, not "demote everyone" — the safety valve
// DEPLOYMENT.md documents.
func TestUnsetAdminEmailsIsNotEnforced(t *testing.T) {
	for name, value := range map[string]string{
		"unset":           "",
		"blank":           "  ",
		"only separators": ",,,",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvAdminEmails, value)
			if LoadAdmins().Enforced() {
				t.Error("Enforced() = true, want false")
			}
		})
	}
}

func TestProvidersNeedBothHalvesOfTheirCredentials(t *testing.T) {
	t.Setenv(EnvGoogleClientID, " google-id ")
	t.Setenv(EnvGoogleClientSecret, " google-secret ")
	t.Setenv(EnvMicrosoftClientID, "microsoft-id")
	t.Setenv(EnvMicrosoftClientSecret, "")

	configured, incomplete := Providers()

	if len(configured) != 1 || configured[0].Name != "google" {
		t.Fatalf("configured = %+v, want google only", configured)
	}
	if configured[0].ClientID != "google-id" || configured[0].ClientSecret != "google-secret" {
		t.Errorf("credentials were not trimmed: %+v", configured[0])
	}
	if !slices.Equal(incomplete, []string{"microsoft"}) {
		t.Errorf("incomplete = %v, want [microsoft]", incomplete)
	}
}

func TestNoProvidersWithoutCredentials(t *testing.T) {
	for _, name := range []string{
		EnvGoogleClientID, EnvGoogleClientSecret,
		EnvMicrosoftClientID, EnvMicrosoftClientSecret,
	} {
		t.Setenv(name, "")
	}

	configured, incomplete := Providers()
	if len(configured) != 0 || len(incomplete) != 0 {
		t.Errorf("configured = %+v, incomplete = %v, want both empty", configured, incomplete)
	}
}

func TestPasswordLoginDefaultsToOff(t *testing.T) {
	t.Setenv(EnvAllowPasswordLogin, "")
	if PasswordLoginEnabled() {
		t.Error("PasswordLoginEnabled() = true with the variable unset, want false")
	}
}

func TestPasswordLoginAcceptsDjangosTruthyValues(t *testing.T) {
	for value, want := range map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "on": true, " True ": true,
		"0": false, "false": false, "no": false, "off": false, "maybe": false,
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(EnvAllowPasswordLogin, value)
			if got := PasswordLoginEnabled(); got != want {
				t.Errorf("PasswordLoginEnabled() = %v for %q, want %v", got, value, want)
			}
		})
	}
}
