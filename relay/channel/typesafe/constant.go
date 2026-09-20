package typesafe

// ModelList holds TypeSafe's aliases. Versioned ids such as jev-1.13.0 are
// accepted by the upstream `model` field whether or not they are listed here,
// so operators can add them to the channel's model list directly.
var ModelList = []string{
	"jev-latest",
	"jev-preview",
}

var ChannelName = "typesafe"
