package main

import (
	"regexp"
	"sync"
)

const (
	domainNameRegexStr = `^(([a-z0-9]{1,63}|[a-z0-9][a-z0-9-]{0,61}[a-z0-9])\.)+[a-z]{1,63}$`

	maxDomainNameLength int = 253
)

var (
	rxDomainName = lazyRegex(domainNameRegexStr)
)

func lazyRegex(str string) func() *regexp.Regexp {
	var regex *regexp.Regexp
	var once sync.Once
	return func() *regexp.Regexp {
		once.Do(func() {
			regex = regexp.MustCompile(str)
		})
		return regex
	}
}

// Ref: https://www.rfc-editor.org/rfc/rfc5321.html#section-2.3.5
func isDomainName(name string) bool {
	if len(name) > maxDomainNameLength || !rxDomainName().MatchString(name) {
		return false
	}

	return true
}
