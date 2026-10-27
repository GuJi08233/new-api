package constant

var StreamingTimeout int
var DifyDebug bool
var MaxFileDownloadMB int
var StreamScannerMaxBufferMB int
var ForceStreamOption bool
var CountToken bool
var GetMediaToken bool
var GetMediaTokenNotStream bool
var UpdateTask bool
var MaxRequestBodyMB int
var AnonymousRequestBodyLimitKB int
var AzureDefaultAPIVersion string
var NotifyLimitCount int
var NotificationLimitDurationMinute int
var GenerateDefaultToken bool
var ErrorLogEnabled bool
var TaskQueryLimit int
var TaskTimeoutMinutes int

// TaskPollMaxFailures fails a task after this many consecutive unrecognized or
// transient poll outcomes. Zero or negative disables the cutoff.
var TaskPollMaxFailures = 20

// Task plugin protocol bridges (Responses / Images) observe a task until it
// finishes; these bound the wait and the observation cadence.
var TaskPluginProtocolTimeoutSeconds = 600
var TaskPluginProtocolTickMilliseconds = 2000
var TaskPluginProtocolTickJitterMilliseconds = 500
var TaskPluginProtocolHeartbeatSeconds = 15

// temporary variable for sora patch, will be removed in future
var TaskPricePatches []string

// TrustedRedirectDomains is a list of trusted domains for redirect URL validation.
// Domains support subdomain matching (e.g., "example.com" matches "sub.example.com").
var TrustedRedirectDomains []string
