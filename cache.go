// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package goatcounter

import (
	"gopkg.in/mgo.v2/bson"
	"zgo.at/goatcounter/cache"
	"zgo.at/zcache"
)

var (
	sitesCacheByID     cache.Cache
	sitesCacheHostname cache.Cache
)

func InitCache() {
	sitesCacheByID = cache.New(zcache.NoExpiration, -1, "si", &Site{})
	sitesCacheHostname = cache.New(zcache.NoExpiration, -1, "sh", int64(0))
}

func (s Site) StoreCache() (cache.Kind, []byte, error) {
	b, err := bson.Marshal(s)
	return cache.KindSite, b, err
}

func (s *Site) FromCache(v []byte) error {
	return bson.Unmarshal(v, s)
}
