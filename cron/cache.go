// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package cron

import (
	"bytes"
	"fmt"
	"strconv"
	"time"

	"zgo.at/goatcounter/cache"
)

var (
	cacheHitCount cache.Cache
	cacheRefCount cache.Cache
)

func InitCache() {
	cacheHitCount = cache.New(1*time.Hour, 5*time.Minute, "hc", &cacheCountEntry{})
	cacheRefCount = cache.New(1*time.Hour, 5*time.Minute, "rc", &cacheCountEntry{})
}

type cacheCountEntry struct{ total, totalUnique int }

func (s cacheCountEntry) StoreCache() (cache.Kind, []byte, error) {
	// TODO: can encode more efficient.
	return cache.KindCount,
		[]byte(strconv.Itoa(s.total) + " " + strconv.Itoa(s.totalUnique)),
		nil
}

func (s *cacheCountEntry) FromCache(v []byte) error {
	i := bytes.IndexRune(v, ' ')
	if i == -1 {
		return fmt.Errorf("no space in %#v", v)
	}

	var err error
	s.total, err = strconv.Atoi(string(v[:i]))
	if err != nil {
		return err
	}

	s.totalUnique, err = strconv.Atoi(string(v[i+1:]))
	return err
}
