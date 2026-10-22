package logs

import (
	"regexp"
	"strings"
)

// Level is how bad a log line is, for its colour: orange warnings, red
// errors. The rules match the web log viewer (frontend/src/features/logs/lines.ts).
type Level string

// Levels. Info, debug and the like are Plain.
const (
	Plain Level = ""
	Warn  Level = "warn"
	Error Level = "error"
)

var (
	// Terminal escape sequences (colours, cursor moves) that some apps log.
	ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// A level the line states itself: JSON ("level":"warn") or logfmt (level=warn).
	field = regexp.MustCompile(`(?i)"(?:level|lvl|severity|loglevel)"\s*:\s*"([a-z]+)"|\b(?:level|lvl|severity)=["']?([a-z]+)`)
	// Kubernetes' own klog format: E1022 10:00:00.123456 ... (I, W, E, F).
	klog = regexp.MustCompile(`^([IWEF])\d{4} \d{2}:\d{2}:\d{2}`)
	// Otherwise, a level word. levelWord skips it when used as a key.
	errorWord = regexp.MustCompile(`(?i)\b(ERROR|ERR|FATAL|PANIC|CRITICAL|CRIT|EMERG|EXCEPTION|Traceback)\b`)
	warnWord  = regexp.MustCompile(`(?i)\b(WARN|WARNING)\b`)
	// After a word, `":` or `=` means it only names a field ("err": or err=).
	keySuffix = regexp.MustCompile(`^(?:"\s*:|=)`)
)

var stated = map[string]Level{
	"error": Error, "err": Error, "fatal": Error, "panic": Error, "dpanic": Error,
	"critical": Error, "crit": Error, "alert": Error, "emerg": Error, "emergency": Error,
	"warn": Warn, "warning": Warn,
	"w": Warn, "e": Error, "f": Error,
}

// StripANSI removes terminal colour codes, which would otherwise mess up the
// screen or show as junk.
func StripANSI(s string) string {
	return ansi.ReplaceAllString(s, "")
}

// LevelOf says how bad a line is. The level it states (JSON, logfmt or klog)
// wins; only lines without one are judged by their words.
func LevelOf(text string) Level {
	if m := field.FindStringSubmatch(text); m != nil {
		word := m[1]
		if word == "" {
			word = m[2]
		}
		return stated[strings.ToLower(word)]
	}
	if m := klog.FindStringSubmatch(text); m != nil {
		return stated[strings.ToLower(m[1])]
	}
	if levelWord(errorWord, text) {
		return Error
	}
	if levelWord(warnWord, text) {
		return Warn
	}
	return Plain
}

// levelWord reports whether re matches text somewhere other than as a key.
func levelWord(re *regexp.Regexp, text string) bool {
	for _, loc := range re.FindAllStringIndex(text, -1) {
		if !keySuffix.MatchString(text[loc[1]:]) {
			return true
		}
	}
	return false
}
