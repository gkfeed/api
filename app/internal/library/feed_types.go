package library

// The API catalog is shared by explicit validation, Jev criteria and discovery.
// Changes to parser capabilities must be reflected here deliberately.
type FeedTypeOption struct {
	Type        FeedType
	Description string
}

var feedTypeOptions = []FeedTypeOption{
	{Type: "web", Description: "A general website, RSS or Atom feed that does not match a more specific type."},
	{Type: "tiktok", Description: "A TikTok creator or feed."},
	{Type: "kinogo", Description: "A Kinogo movie or TV source."},
	{Type: "twitch", Description: "A Twitch channel."},
	{Type: "yummyanime", Description: "A YummyAnime source."},
	{Type: "shiki", Description: "A Shikimori anime or manga source."},
	{Type: "reddit", Description: "A Reddit community or user feed."},
	{Type: "vk", Description: "A VK community, profile or video source."},
	{Type: "yt", Description: "A YouTube channel or creator feed."},
	{Type: "ranobe.me", Description: "A Ranobe.me light novel source."},
	{Type: "spoti", Description: "A Spotify artist feed."},
	{Type: "rezka", Description: "A Rezka movie or TV source."},
	{Type: "inst", Description: "An Instagram profile or posts feed."},
	{Type: "stories", Description: "An Instagram stories feed."},
	{Type: "insolarance", Description: "An Insolarance source."},
	{Type: "mangalib", Description: "A MangaLib title or feed."},
	{Type: "x", Description: "An X or Twitter profile feed."},
	{Type: "spoti:playlist", Description: "A Spotify playlist."},
	{Type: "onefootball", Description: "A OneFootball team, competition or match source."},
	{Type: "rtl", Description: "An RTL source."},
	{Type: "rezka:collection", Description: "A Rezka collection rather than one movie or TV title."},
	{Type: "matreshka", Description: "A Matreshka video source."},
	{Type: "shiki:ongoing", Description: "A Shikimori ongoing releases feed."},
	{Type: "anilibria", Description: "An AniLibria anime source."},
	{Type: "pornhub", Description: "A PornHub creator or video feed."},
	{Type: "hltv", Description: "An HLTV team, event, news or match source."},
	{Type: "liquidpedia", Description: "A Liquipedia esports source."},
	{Type: "sasflix", Description: "A Sasflix movie or TV source."},
	{Type: "porno365", Description: "A Porno365 source."},
}

func FeedTypeOptions() []FeedTypeOption {
	return append([]FeedTypeOption(nil), feedTypeOptions...)
}

func IsFeedType(value FeedType) bool {
	for _, option := range feedTypeOptions {
		if option.Type == value {
			return true
		}
	}
	return false
}
