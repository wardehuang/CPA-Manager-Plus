package automation

import (
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
)

func TestStatusExposesEffectiveFlagsAndKeys(t *testing.T) {
	cfg := config.Config{
		QuotaCooldownEnabled:      true,
		AccountActionsEnabled:     true,
		AccountActionsAutoDisable: false,
	}
	status := New(cfg).Status()

	if status.Source != SourceStartup {
		t.Fatalf("source = %q, want %q", status.Source, SourceStartup)
	}

	if !status.QuotaCooldown.Enabled || status.QuotaCooldown.EnvKey != "USAGE_QUOTA_COOLDOWN_ENABLED" || status.QuotaCooldown.ConfigFileKey != "quotaCooldownEnabled" {
		t.Fatalf("quotaCooldown = %#v", status.QuotaCooldown)
	}
	if status.QuotaCooldown.DependsOn != "" {
		t.Fatalf("quotaCooldown should not declare a dependency, got %q", status.QuotaCooldown.DependsOn)
	}

	if !status.AccountActions.Enabled || status.AccountActions.EnvKey != "USAGE_ACCOUNT_ACTIONS_ENABLED" || status.AccountActions.ConfigFileKey != "accountActionsEnabled" {
		t.Fatalf("accountActions = %#v", status.AccountActions)
	}
	if status.AccountActions.DependsOn != "" {
		t.Fatalf("accountActions should not declare a dependency, got %q", status.AccountActions.DependsOn)
	}

	if status.AccountActionsAutoDisable.Enabled {
		t.Fatalf("accountActionsAutoDisable should be disabled, got %#v", status.AccountActionsAutoDisable)
	}
	if status.AccountActionsAutoDisable.EnvKey != "USAGE_ACCOUNT_ACTIONS_AUTO_DISABLE" || status.AccountActionsAutoDisable.ConfigFileKey != "accountActionsAutoDisable" {
		t.Fatalf("accountActionsAutoDisable keys = %#v", status.AccountActionsAutoDisable)
	}
	if status.AccountActionsAutoDisable.DependsOn != "accountActions" {
		t.Fatalf("accountActionsAutoDisable dependsOn = %q", status.AccountActionsAutoDisable.DependsOn)
	}
}

func TestStatusAutoDisableReportsEffectiveValue(t *testing.T) {
	status := New(config.Config{
		AccountActionsEnabled:     false,
		AccountActionsAutoDisable: true,
	}).Status()
	if status.AccountActions.Enabled {
		t.Fatalf("accountActions should be disabled, got %#v", status.AccountActions)
	}
	if status.AccountActionsAutoDisable.Enabled {
		t.Fatalf("accountActionsAutoDisable should not be effective when accountActions is disabled, got %#v", status.AccountActionsAutoDisable)
	}
	if status.AccountActionsAutoDisable.DependsOn != "accountActions" {
		t.Fatalf("accountActionsAutoDisable dependsOn = %q", status.AccountActionsAutoDisable.DependsOn)
	}

	status = New(config.Config{
		AccountActionsEnabled:     true,
		AccountActionsAutoDisable: true,
	}).Status()
	if !status.AccountActionsAutoDisable.Enabled {
		t.Fatalf("accountActionsAutoDisable should be effective when accountActions is enabled, got %#v", status.AccountActionsAutoDisable)
	}
}

func TestStatusDefaultsAllOff(t *testing.T) {
	status := New(config.Config{}).Status()
	if status.QuotaCooldown.Enabled || status.AccountActions.Enabled || status.AccountActionsAutoDisable.Enabled {
		t.Fatalf("expected all capabilities disabled by default, got %#v", status)
	}
}
