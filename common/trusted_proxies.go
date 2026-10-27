package common

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// ResolveTrustedProxies parses TRUSTED_PROXIES without applying it to an
// engine, so the main engine and the task plugin engine share one rule. An
// empty value trusts no forwarding proxy: Gin's default of trusting every
// proxy would let clients spoof the IP that IP restrictions and risk control
// rely on.
func ResolveTrustedProxies(raw string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	proxies := make([]string, 0, len(parts))
	for _, part := range parts {
		if proxy := strings.TrimSpace(part); proxy != "" {
			proxies = append(proxies, proxy)
		}
	}
	if len(proxies) == 0 {
		return nil, nil
	}
	return proxies, nil
}

func ConfigureTrustedProxies(engine *gin.Engine, trustedProxies []string) error {
	if err := engine.SetTrustedProxies(trustedProxies); err != nil {
		return fmt.Errorf("invalid TRUSTED_PROXIES: %w", err)
	}
	return nil
}
