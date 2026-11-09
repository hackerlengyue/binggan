package playerautomation

// Playback-button geometry comes from PlayerControlBar's nib and viewDidLoad.
// Both versions retain the progress/clock identifiers used by the orchestrator.
type playerProfile struct {
	playbackRelativeX float64
	playbackOffsetX   float64
	playbackOffsetY   float64
}

func profileForVersion(version string) (playerProfile, bool) {
	switch version {
	case "26.06.54":
		return playerProfile{0, 597, 22.5}, true
	case "26.10.56":
		// The 300-point toolbar is centered in the bar. Its first 18-point
		// cell is play/pause; the slider has symmetric 3-point side insets.
		return playerProfile{0.5, -141, 23.5}, true
	default:
		return playerProfile{}, false
	}
}
