package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	steamOpenIDEndpoint       = "https://steamcommunity.com/openid/login"
	steamPlayerSummaryAPI     = "http://api.steampowered.com/ISteamUser/GetPlayerSummaries/v0002/"
	steamClaimedIDPattern     = `^https?://steamcommunity\.com/openid/id/(\d{15,25})$`
)

func init() {
	Register("steam", &SteamProvider{})
}

// SteamProvider implements OpenID 2.0 authentication for Steam
type SteamProvider struct{}

type steamPlayerSummaryResponse struct {
	Response struct {
		Players []steamPlayer `json:"players"`
	} `json:"response"`
}

type steamPlayer struct {
	SteamID      string `json:"steamid"`
	PersonaName  string `json:"personaname"`
	RealName     string `json:"realname"`
	AvatarFull   string `json:"avatarfull"`
	LocCountryCode string `json:"loccountrycode"`
}

func (p *SteamProvider) GetName() string {
	return "Steam"
}

func (p *SteamProvider) IsEnabled() bool {
	return common.SteamOAuthEnabled
}

// ExchangeToken validates the OpenID 2.0 response from Steam.
// The code parameter contains the entire OpenID query string.
func (p *SteamProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	logger.LogDebug(ctx, "[OAuth-Steam] ExchangeToken: validating OpenID response")

	// Parse the OpenID parameters from the query string
	params, err := url.ParseQuery(code)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: failed to parse query: %s", err.Error()))
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, map[string]any{"Provider": "Steam"})
	}

	// Verify openid.mode is "id_res"
	mode := params.Get("openid.mode")
	if mode != "id_res" {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: unexpected openid.mode=%s", mode))
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, map[string]any{"Provider": "Steam"})
	}

	// Verify openid.return_to contains our callback path
	returnTo := params.Get("openid.return_to")
	if returnTo == "" || !strings.Contains(returnTo, "/oauth/steam") {
		logger.LogError(ctx, "[OAuth-Steam] ExchangeToken: invalid or missing openid.return_to")
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, map[string]any{"Provider": "Steam"})
	}

	// Build verification request
	verifyValues := make(url.Values)
	verifyValues.Set("openid.assoc_handle", params.Get("openid.assoc_handle"))
	verifyValues.Set("openid.signed", params.Get("openid.signed"))
	verifyValues.Set("openid.sig", params.Get("openid.sig"))
	verifyValues.Set("openid.ns", params.Get("openid.ns"))

	// Add all signed fields
	signedFields := strings.Split(params.Get("openid.signed"), ",")
	for _, field := range signedFields {
		field = strings.TrimSpace(field)
		if field != "" {
			verifyValues.Set("openid."+field, params.Get("openid."+field))
		}
	}
	verifyValues.Set("openid.mode", "check_authentication")

	// POST back to Steam to verify the response
	client := http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", steamOpenIDEndpoint, strings.NewReader(verifyValues.Encode()))
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: failed to create verify request: %s", err.Error()))
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: verify request failed: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Steam"}, err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: failed to read verify response: %s", err.Error()))
		return nil, err
	}

	responseText := string(body)
	if !strings.Contains(responseText, "is_valid:true") {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: OpenID verification failed: %s", responseText))
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Steam"})
	}

	// Extract Steam ID from openid.claimed_id
	claimedID := params.Get("openid.claimed_id")
	re := regexp.MustCompile(steamClaimedIDPattern)
	matches := re.FindStringSubmatch(claimedID)
	if len(matches) < 2 {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] ExchangeToken: invalid claimed_id: %s", claimedID))
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, map[string]any{"Provider": "Steam"})
	}

	steamID := matches[1]
	logger.LogDebug(ctx, "[OAuth-Steam] ExchangeToken success: steam_id=%s", steamID)

	return &OAuthToken{
		AccessToken: steamID,
	}, nil
}

// GetUserInfo fetches the player profile from Steam Web API.
func (p *SteamProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	steamID := token.AccessToken
	apiKey := common.SteamWebApiKey

	if apiKey == "" {
		logger.LogError(ctx, "[OAuth-Steam] GetUserInfo: Steam Web API Key is not configured")
		return nil, NewOAuthError(i18n.MsgOAuthNotEnabled, map[string]any{"Provider": "Steam"})
	}

	apiURL := fmt.Sprintf("%s?key=%s&steamids=%s", steamPlayerSummaryAPI, url.QueryEscape(apiKey), url.QueryEscape(steamID))
	logger.LogDebug(ctx, "[OAuth-Steam] GetUserInfo: fetching player summary for steam_id=%s", steamID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] GetUserInfo: request failed: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Steam"}, err.Error())
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] GetUserInfo: status=%d, body=%s", res.StatusCode, bodyStr))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthGetUserErr, map[string]any{"Provider": "Steam"}, fmt.Sprintf("status %d", res.StatusCode))
	}

	var summaryResp steamPlayerSummaryResponse
	if err := common.DecodeJson(res.Body, &summaryResp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Steam] GetUserInfo: decode error: %s", err.Error()))
		return nil, err
	}

	if len(summaryResp.Response.Players) == 0 {
		logger.LogError(ctx, "[OAuth-Steam] GetUserInfo: no player data returned")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "Steam"})
	}

	player := summaryResp.Response.Players[0]
	logger.LogDebug(ctx, "[OAuth-Steam] GetUserInfo success: steam_id=%s, persona=%s", player.SteamID, player.PersonaName)

	return &OAuthUser{
		ProviderUserID: player.SteamID,
		Username:       "",
		DisplayName:    player.PersonaName,
		Extra: map[string]any{
			"avatar":      player.AvatarFull,
			"persona":     player.PersonaName,
		},
	}, nil
}

func (p *SteamProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsSteamOpenIdAlreadyTaken(providerUserID)
}

func (p *SteamProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.SteamOpenId = providerUserID
	return user.FillUserBySteamOpenId()
}

func (p *SteamProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.SteamOpenId = providerUserID
}

func (p *SteamProvider) GetProviderPrefix() string {
	return "steam_"
}
