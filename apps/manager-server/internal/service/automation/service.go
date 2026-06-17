package automation

import "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"

// SourceStartup 表示自动化开关的取值来源标签。
// 当前 config loader 没有逐字段记录 env/config/default 来源，所以第一版统一展示为 startup
// （由启动配置决定），前端据此提示用户通过 env / config.json 修改。
const SourceStartup = "startup"

// Capability 描述单个自动化能力的只读状态。
type Capability struct {
	Enabled       bool   `json:"enabled"`
	EnvKey        string `json:"envKey"`
	ConfigFileKey string `json:"configFileKey"`
	// DependsOn 表示该能力依赖另一个能力先开启，例如 accountActionsAutoDisable 依赖 accountActions。
	DependsOn string `json:"dependsOn,omitempty"`
}

// Status 是自动化设置只读状态的整体响应。
type Status struct {
	Source                    string     `json:"source"`
	QuotaCooldown             Capability `json:"quotaCooldown"`
	AccountActions            Capability `json:"accountActions"`
	AccountActionsAutoDisable Capability `json:"accountActionsAutoDisable"`
}

// Service 基于启动 config 组装自动化能力的只读状态。它不修改任何配置，也不参与 worker 行为。
type Service struct {
	cfg config.Config
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg}
}

// Status 返回三个自动化能力的当前有效值与配置键名。
// accountActionsAutoDisable 的 Enabled 是实际生效值：只有 accountActions 开启时，
// AccountActionCandidateWorker 才会启动，自动禁用才可能生效。
func (s *Service) Status() Status {
	accountActionsEnabled := s.cfg.AccountActionsEnabled
	return Status{
		Source: SourceStartup,
		QuotaCooldown: Capability{
			Enabled:       s.cfg.QuotaCooldownEnabled,
			EnvKey:        "USAGE_QUOTA_COOLDOWN_ENABLED",
			ConfigFileKey: "quotaCooldownEnabled",
		},
		AccountActions: Capability{
			Enabled:       accountActionsEnabled,
			EnvKey:        "USAGE_ACCOUNT_ACTIONS_ENABLED",
			ConfigFileKey: "accountActionsEnabled",
		},
		AccountActionsAutoDisable: Capability{
			Enabled:       accountActionsEnabled && s.cfg.AccountActionsAutoDisable,
			EnvKey:        "USAGE_ACCOUNT_ACTIONS_AUTO_DISABLE",
			ConfigFileKey: "accountActionsAutoDisable",
			DependsOn:     "accountActions",
		},
	}
}
