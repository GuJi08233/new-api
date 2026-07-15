package operation_setting

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type RouteTierCondition struct {
	Var   string `json:"var"`   // "len" | "p" | "c"
	Op    string `json:"op"`    // "<" | "<=" | ">" | ">="
	Value int    `json:"value"` // token count
}

type RouteTier struct {
	Conditions []RouteTierCondition `json:"conditions,omitempty"` // 0~2, AND
	ChannelIDs []int                `json:"channel_ids"`
	Label      string               `json:"label,omitempty"`
}

type ChannelRouteRule struct {
	GroupRegex []string    `json:"group_regex,omitempty"`
	Name       string      `json:"name"`
	ModelRegex []string    `json:"model_regex"`
	PathRegex  []string    `json:"path_regex"`
	ChannelIDs []int       `json:"channel_ids,omitempty"` // Deprecated: legacy fallback pool, kept for backward compatibility. Use RouteTiers with an empty-conditions tier instead.
	Strict     bool        `json:"strict"`
	RouteTiers []RouteTier `json:"route_tiers,omitempty"` // tiered routing; a tier with empty Conditions acts as the catch-all (fallback)
}

type ChannelRouteSetting struct {
	Enabled bool               `json:"enabled"`
	Rules   []ChannelRouteRule `json:"rules"`
}

// channelRouteSetting is only a staging area mutated in place by the config
// framework (reflection-based, unsynchronized). Request goroutines must never
// read it directly; they read the immutable snapshot published below.
var channelRouteSetting = ChannelRouteSetting{
	Enabled: false,
	Rules:   []ChannelRouteRule{},
}

var channelRouteSnapshot atomic.Pointer[ChannelRouteSetting]

func init() {
	config.GlobalConfig.Register("channel_route_setting", &channelRouteSetting)
	SyncChannelRouteSetting()
}

func GetChannelRouteSetting() *ChannelRouteSetting {
	return channelRouteSnapshot.Load()
}

// SyncChannelRouteSetting publishes a deep copy of the staging config as the
// read-only snapshot. Must be called after every update to the staging struct
// (see handleConfigUpdate in model/option.go).
func SyncChannelRouteSetting() {
	snapshot := &ChannelRouteSetting{}
	data, err := common.Marshal(channelRouteSetting)
	if err != nil || common.Unmarshal(data, snapshot) != nil {
		snapshot = &ChannelRouteSetting{Enabled: channelRouteSetting.Enabled}
	}
	channelRouteSnapshot.Store(snapshot)
}
