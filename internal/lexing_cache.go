package internal

const tokenCacheSize = 1024
const tokenCacheMask = tokenCacheSize - 1

type cacheEntry struct {
	key   uint64
	token Token
	valid bool
}

type TokenCache struct {
	entries [tokenCacheSize]cacheEntry
}

func TokenCacheCreate() *TokenCache {
	return &TokenCache{entries: [1024]cacheEntry{}}
}

func TokenCachePut(cache *TokenCache, key uint64, token *Token) {
	idx := key & tokenCacheMask
	cache.entries[idx] = cacheEntry{
		key:   key,
		token: *token,
		valid: true,
	}
}

func TokenCacheGet(cache *TokenCache, key uint64) (*Token, bool) {
	idx := key & tokenCacheMask
	entry := &cache.entries[idx]

	if entry.valid && entry.key == key {
		return &entry.token, true
	}

	return nil, false
}

func TokenCacheClear(cache *TokenCache) {
	clear(cache.entries[:])
}
