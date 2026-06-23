package player

import "os"

// ffmpegBin and ffprobeBin resolve the transcoder binaries. In a packaged build
// the Electron shell sets FFMPEG_PATH / FFPROBE_PATH to the bundled
// (ffmpeg-static / ffprobe-static) copies, so playback works even when the user
// has no system ffmpeg and when the app is GUI-launched with a stripped-down
// PATH. In dev / plain CLI runs the vars are unset and we fall back to PATH.
func ffmpegBin() string {
	if p := os.Getenv("FFMPEG_PATH"); p != "" {
		return p
	}
	return "ffmpeg"
}

func ffprobeBin() string {
	if p := os.Getenv("FFPROBE_PATH"); p != "" {
		return p
	}
	return "ffprobe"
}
