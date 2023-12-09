package util

import "hash/fnv"

func GetShardID(value string, shardCount int) int {
	h := fnv.New32a()
	h.Write([]byte(value))
	return int(h.Sum32() % uint32(shardCount))
}
