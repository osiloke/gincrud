package gincrud

import (
	"encoding/json"
	"fmt"
	"github.com/fatih/structs"
	"github.com/gin-gonic/gin"
	"github.com/osiloke/gostore"
	"strconv"
)

type ErrorCtx struct {
	Bucket string
	key    string
	GinCtx *gin.Context
}

type SuccessCtx struct {
	Bucket string
	Key    string
	Result map[string]interface{}
	GinCtx *gin.Context
}
type GetKey func(interface{}, *gin.Context) string
type OnSuccess func(ctx SuccessCtx) (string, error)
type OnError func(ctx interface{}, err error) error

type Results struct {
	Data  []map[string]interface{} `json:"data"`
	Count int                      `json:"count,omitempty"`
}

//TODO: Extract core logic from each crud function i.e make doGetAll, doGet, ... they return data, err
func GetAll(bucket string, store gostore.Store, c *gin.Context, onSuccess OnSuccess, onError OnError) {
	var results []map[string]interface{}
	var err error

	count := 10
	q := c.Request.URL.Query()
	if val, ok := q["_perPage"]; ok {
		count, _ = strconv.Atoi(val[0])
	}
	var data [][][]byte

	if val, ok := q["afterKey"]; ok {
		data, err = store.GetAllAfter([]byte(val[0]), count+1, 0, bucket)
	} else if val, ok := q["beforeKey"]; ok {
		data, err = store.GetAllBefore([]byte(val[0]), count+1, 0, bucket)
	} else {
		data, err = store.GetAll(count+1, 0, bucket)
	}
	if err != nil {
		if onError != nil {
			onError(ErrorCtx{Bucket: bucket, GinCtx: c}, err)
		}
		c.JSON(200, []string{})
	} else {
		for _, element := range data {
			var result map[string]interface{}
			_ = json.Unmarshal(element[1], &result)
			result["key"] = string(element[0])
			results = append(results, result)
		}
		if len(results) == 0 {
			c.JSON(200, []string{})
		} else {
			if onSuccess != nil {
			}
			stats, _ := store.Stats(bucket)
			c.Writer.Header().Set("X-Total-Count", fmt.Sprintf("%d", stats["KeyN"]))
			c.JSON(200, Results{results, count})
			// c.JSON(200, results)
		}
	}
}

func Get(key, bucket string, store gostore.Store, c *gin.Context, record interface{}, onSuccess OnSuccess, onError OnError) {
	data, err := store.Get([]byte(key), bucket)
	if err != nil {
		//TODO: Does not exist error for store
		if onError != nil {
			onError(ErrorCtx{bucket, key, c}, err)
		}
		fmt.Println("Could not retrieve", key, "from", bucket)
		c.JSON(404, gin.H{"msg": fmt.Sprintf("Not found")})
	} else {
		_ = json.Unmarshal(data[1], record)
		m := structs.Map(record)
		kk := string(data[0])
		m["key"] = kk
		if onSuccess != nil {
			ctx := SuccessCtx{bucket, kk, m, c}
			onSuccess(ctx)
		}
		c.JSON(200, m)
	}
}

func Post(bucket string, store gostore.Store, c *gin.Context, record interface{}, fn GetKey, onSuccess OnSuccess, onError OnError) {
	if b := c.Bind(record); b != false {
		m := structs.Map(record)
		data, err := json.Marshal(&record)
		key := fn(m, c)
		if err != nil {
			onError(ErrorCtx{Bucket: bucket}, err)
			c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
		} else {
			store.Save([]byte([]byte(key)), data, bucket)
			m["key"] = key

			if onSuccess != nil {
				ctx := SuccessCtx{bucket, key, m, c}
				onSuccess(ctx)
			}
			c.JSON(200, m)
		}
	} else {
		c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly"})
	}
}

func Put(key, bucket string, store gostore.Store, c *gin.Context, record interface{}, onSuccess OnSuccess, onError OnError) {
	if b := c.Bind(record); b != false {
		data, err := json.Marshal(&record)
		if err != nil {
			if onError != nil {
				onError(ErrorCtx{bucket, key, c}, err)
			}
			c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
		} else {
			store.Save([]byte([]byte(key)), data, bucket)
			m := structs.Map(record)
			if onSuccess != nil {
				ctx := SuccessCtx{bucket, key, m, c}
				onSuccess(ctx)
			}
			c.JSON(200, m)
		}
	} else {
		c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly"})
	}
}

func Delete(key, bucket string, store gostore.Store, c *gin.Context, onSuccess OnSuccess, onError OnError) {
	err := store.Delete([]byte(key), bucket)
	if err != nil {
		if onError != nil {
			onError(ErrorCtx{bucket, key, c}, err)
		}
		c.JSON(500, gin.H{"msg": "The item [" + key + "] was not deleted"})
	} else {
		if onSuccess != nil {
			ctx := SuccessCtx{Bucket: bucket, Key: key, GinCtx: c}
			onSuccess(ctx)
		}
		c.JSON(200, gin.H{"msg": "The item [" + key + "] was deleted"})
	}
}
