package main

import (
	"C"
	"fmt"
	"unsafe"

	"github.com/mwindower/fluent-bit-go/output"
)
import (
	"encoding/json"
	"os"
	"time"
)

var (
	rc *redisClient
)

//export FLBPluginRegister
func FLBPluginRegister(ctx unsafe.Pointer) int {
	return output.FLBPluginRegister(ctx, "redis", "Redis Output Plugin.")
}

//export FLBPluginInit
// ctx (context) pointer to fluentbit context (state/ c code)
func FLBPluginInit(ctx unsafe.Pointer) int {
	hosts := output.FLBPluginConfigKey(ctx, "Hosts")
	password := output.FLBPluginConfigKey(ctx, "Password")
	key := output.FLBPluginConfigKey(ctx, "Key")
	db := output.FLBPluginConfigKey(ctx, "DB")
	usetls := output.FLBPluginConfigKey(ctx, "UseTLS")
	tlsskipverify := output.FLBPluginConfigKey(ctx, "TLSSkipVerify")

	// create a pool of redis connection pools
	config, err := getRedisConfig(hosts, password, db, usetls, tlsskipverify, key)
	if err != nil {
		output.Errorf(ctx, "configuration errors: %v\n", err)
		// FIXME use fluent-bit method to err in init
		output.FLBPluginUnregister(ctx)
		os.Exit(1)
	}
	redisPools, err := newPoolsFromConfig(config)
	if err != nil {
		output.Errorf(ctx, "cannot create a pool of redis connections: %v\n", err)
		output.FLBPluginUnregister(ctx)
		os.Exit(1)
	}

	rc = &redisClient{
		pools: redisPools,
		key:   config.key,
	}

	output.Infof(ctx, "established connection to redis pool: %v\n", redisPools)
	return output.FLB_OK
}

//export FLBPluginFlush
// FLBPluginFlush is called from fluent-bit when data need to be sent. is called from fluent-bit when data need to be sent.
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	var ret int
	var ts interface{}
	var record map[interface{}]interface{}

	// Create Fluent Bit decoder
	dec := output.NewDecoder(data, int(length))

	// Iterate Records
	for {
		// Extract Record
		ret, ts, record = output.GetRecord(dec)
		if ret != 0 {
			break
		}

		// Print record keys and values
		// convert timestamp to RFC3339Nano which is logstash format
		timestamp := ts.(output.FLBTime)
		js, err := createJSON(timestamp.String(), C.GoString(tag), record)
		if err != nil {
			fmt.Printf("%v\n", err)
			return output.FLB_RETRY
		}
		err = rc.write(js)
		if err != nil {
			fmt.Printf("%v\n", err)
			return output.FLB_RETRY
		}
	}

	// Return options:
	//
	// output.FLB_OK    = data have been processed.
	// output.FLB_ERROR = unrecoverable error, do not try this again.
	// output.FLB_RETRY = retry to flush later.
	return output.FLB_OK
}

func createJSON(timestamp string, tag string, record map[interface{}]interface{}) ([]byte, error) {
	// convert timestamp to RFC3339Nano which is logstash format
	const timeFormat = "2006-01-02 15:04:05.999999999 -0700 MST"
	t, _ := time.Parse(timeFormat, timestamp)
	m := make(map[string]interface{})
	m["@timestamp"] = t.UTC().Format(time.RFC3339Nano)
	m["@tag"] = tag
	for k, v := range record {
		switch t := v.(type) {
		case []byte:
			// prevent encoding to base64
			m[k.(string)] = string(t)
		default:
			m[k.(string)] = v
		}
	}
	js, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("error creating message for REDIS: %v", err)
	}
	return js, nil
}

//export FLBPluginExit
func FLBPluginExit() int {
	rc.pools.closeAll()
	return output.FLB_OK
}

func main() {
}
