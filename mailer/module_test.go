package mailer

import (
	"testing"

	"github.com/netgarden/maf"
)

// TestGetConfigSchema_NeitherHostNorFromIsRequired proves neither
// mailer.smtp.host nor mailer.smtp.from is a schema-level Required item —
// see validateSMTPFrom for why smtp.from's requirement is instead enforced
// in code, conditional on smtp.host being set.
func TestGetConfigSchema_NeitherHostNorFromIsRequired(t *testing.T) {
	schema := NewModule().GetConfigSchema()

	var host, from *maf.ConfigItem
	for i := range schema {
		switch schema[i].Name {
		case "mailer.smtp.host":
			host = &schema[i]
		case "mailer.smtp.from":
			from = &schema[i]
		}
	}

	if host == nil {
		t.Fatal("expected mailer.smtp.host to be present in the config schema")
	}
	if host.Required {
		t.Error("expected mailer.smtp.host to not be required")
	}

	if from == nil {
		t.Fatal("expected mailer.smtp.from to be present in the config schema")
	}
	if from.Required {
		t.Error("expected mailer.smtp.from to not be required")
	}
}

func TestValidateSMTPFrom(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		from      string
		wantError bool
	}{
		{name: "both unset: queue-only mode, ok", host: "", from: "", wantError: false},
		{name: "host set, from set: ok", host: "smtp.example.com", from: "citadel@example.com", wantError: false},
		{name: "host set, from unset: rejected", host: "smtp.example.com", from: "", wantError: true},
		{name: "host unset, from set: ok (from is simply unused)", host: "", from: "citadel@example.com", wantError: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSMTPFrom(c.host, c.from)
			if c.wantError && err == nil {
				t.Error("expected an error, got nil")
			}
			if !c.wantError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}
