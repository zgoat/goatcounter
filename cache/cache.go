// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package cache

import (
	"fmt"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"zgo.at/goatcounter/cfg"
	"zgo.at/zcache"
	"zgo.at/zlog"
)

type Cache interface {
	SetDefault(k string, v interface{})
	Get(k string) (interface{}, bool)
	Flush()
	Delete(k string)
}

var l = zlog.Module("cache")

func New(defaultExpiration, cleanupInterval time.Duration, keyPrefix string, kind interface{}) Cache {
	if cfg.Memcached != "" {
		l.Debugf("using memcached at %s", cfg.Memcached)
		mcOnce.Do(func() {
			mcConn = memcache.New(cfg.Memcached)
			err := mcConn.Ping()
			if err != nil {
				panic(fmt.Sprintf("cannot connect to memcached at %s: %s", cfg.Memcached, err))
			}
		})
		return &memcached{mc: mcConn, keyPrefix: keyPrefix, t: kind}
	}
	l.Debug("using in-memory cache")
	return zcache.New(defaultExpiration, cleanupInterval)
}
