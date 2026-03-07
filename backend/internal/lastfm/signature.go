package lastfm

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
)

func BuildAPISig(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	s := ""
	for _, k := range keys {
		if k == "format" {
			continue
		}
		s += k + params[k]
	}
	s += secret

	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
