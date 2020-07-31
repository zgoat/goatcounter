// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

package cache

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/bradfitz/gomemcache/memcache"
	"zgo.at/zlog"
)

var (
	mcConn *memcache.Client
	mcOnce sync.Once
)

type Kind byte

const (
	KindInt64 = 'i'
	KindSite  = 's'
	KindCount = 'c'
)

// Memcached cache.
type memcached struct {
	mc        *memcache.Client
	keyPrefix string
	t         interface{}
}

type cacher interface {
	StoreCache() (Kind, []byte, error)
	FromCache([]byte) error
}

func (c *memcached) key(k string) string {
	// Maximum key length is 250 character nd must not include control
	// characters or whitespace.
	k = c.keyPrefix + strings.ReplaceAll(k, " ", "_")
	if len(k) > 250 {
		h := sha256.New()
		h.Write([]byte(k))
		hash := fmt.Sprintf("%x", h.Sum(nil)) // TODO: can do this better.
		k = k[:250-len(hash)] + hash
	}
	return k
}

func (c *memcached) Get(k string) (interface{}, bool) {
	k = c.key(k)

	item, err := c.mc.Get(k)
	if err != nil {
		if err != memcache.ErrCacheMiss {
			l.Field("key", k).Errorf("Get: %w", err)
		}
		return nil, false
	}

	var r interface{}
	switch Kind(item.Flags) {
	case KindInt64:
		i, err := strconv.ParseInt(string(item.Value), 10, 64)
		if err != nil {
			l.Field("v", item.Value).Errorf("Get: %w", err)
			return nil, false
		}
		r = i
	case KindSite:
		st := c.t.(cacher)
		err := st.FromCache(item.Value)
		if err != nil {
			l.Field("v", item.Value).Errorf("Get: site: %w", err)
			return nil, false
		}
		r = st
	case KindCount:
		st := c.t.(cacher)
		err := st.FromCache(item.Value)
		if err != nil {
			l.Field("v", item.Value).Errorf("Get: Count: %w", err)
			return nil, false
		}
		r = st
	default:
		l.Field("key", k).Errorf("GET: unhandled type: %#v", item.Value)
		return nil, false
	}

	l.Fields(zlog.F{"key": k, "r": r}).Debug("Get")
	return r, true
}

func (c *memcached) SetDefault(k string, v interface{}) {
	k = c.key(k)

	var (
		toset []byte
		kind  Kind
	)
	switch vv := v.(type) {
	case int64:
		toset = []byte(strconv.FormatInt(vv, 10))
		kind = KindInt64
	case cacher:
		k, data, err := vv.StoreCache()
		if err != nil {
			l.Field("key", k).Errorf("SetDefault: StoreCache: %w", err)
			return
		}
		toset = data
		kind = k
	default:
		l.Field("key", k).Errorf("SetDefault: unhandled type %T", vv)
		return
	}

	l.Fields(zlog.F{"key": k, "kind": kind, "toset": toset}).Debug("SetDefault")
	err := c.mc.Set(&memcache.Item{
		Key:   k,
		Value: toset,
		Flags: uint32(kind),
	})
	if err != nil {
		l.Errorf("SetDefault: %w", err)
	}
}

func (c *memcached) Delete(k string) {
	k = c.key(k)

	err := c.mc.Delete(k)
	if err != nil && err != memcache.ErrCacheMiss {
		l.Error(err)
	}
}

func (c *memcached) Flush() {
	// TODO: only delete this keyPrefix
	err := c.mc.FlushAll()
	if err != nil {
		l.Error(err)
	}
}
