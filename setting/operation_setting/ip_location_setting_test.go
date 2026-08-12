package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateIpLocationOption pins the config contract: provider order must be
// a JSON array of known providers without duplicates, and unknown fields are
// rejected before reaching the database.
func TestValidateIpLocationOption(t *testing.T) {
	assert.NoError(t, ValidateIpLocationOption("unrelated_key", "whatever"))
	assert.NoError(t, ValidateIpLocationOption(IpLocationSettingPrefix+"gitee_api_key", ""))
	assert.NoError(t, ValidateIpLocationOption(IpLocationSettingPrefix+"ipv4_order", `["gitee","ipwhois","ip9"]`))
	assert.NoError(t, ValidateIpLocationOption(IpLocationSettingPrefix+"ipv6_order", `["ipwhois"]`))
	assert.NoError(t, ValidateIpLocationOption(IpLocationSettingPrefix+"ipv6_order", `[]`))
	assert.NoError(t, ValidateIpLocationOption(IpLocationSettingPrefix+"auto_lookup", "true"))
	assert.NoError(t, ValidateIpLocationOption(IpLocationSettingPrefix+"auto_lookup", "false"))

	assert.Error(t, ValidateIpLocationOption(IpLocationSettingPrefix+"ipv4_order", `["bogus"]`))
	assert.Error(t, ValidateIpLocationOption(IpLocationSettingPrefix+"ipv4_order", `["gitee","gitee"]`))
	assert.Error(t, ValidateIpLocationOption(IpLocationSettingPrefix+"ipv4_order", `"gitee"`))
	assert.Error(t, ValidateIpLocationOption(IpLocationSettingPrefix+"auto_lookup", "yes"))
	assert.Error(t, ValidateIpLocationOption(IpLocationSettingPrefix+"unknown_field", "x"))
}

// TestResolvedOrderFallsBackToDefaults keeps zero-config deployments working:
// an empty stored order must resolve to the built-in provider chain.
func TestResolvedOrderFallsBackToDefaults(t *testing.T) {
	empty := &IpLocationSetting{}
	assert.Equal(t, defaultIpv4Order, empty.ResolvedIpv4Order())
	assert.Equal(t, defaultIpv6Order, empty.ResolvedIpv6Order())

	custom := &IpLocationSetting{Ipv4Order: []string{IpLocationProviderIp9}}
	assert.Equal(t, []string{IpLocationProviderIp9}, custom.ResolvedIpv4Order())
}
