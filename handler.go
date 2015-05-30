package gincrud

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fatih/structs"
	"github.com/gin-gonic/gin"
	"github.com/mgutz/logxi/v1"
	"github.com/osiloke/gostore"
	"reflect"
	"runtime"
	"strconv"
	"time"
)

var logger = log.New("gincrud")

type ErrorList struct {
	Msg   string                 `json:"msg"`
	Error map[string]interface{} `json:"error"`
}

type ErrorCtx struct {
	Bucket string
	key    string
	GinCtx *gin.Context
}

type SuccessCtx struct {
	Bucket   string
	Key      string
	Existing map[string]interface{}
	Result   map[string]interface{}
	GinCtx   *gin.Context
}
type MarshalError struct {
	Data map[string]interface{}
}

type ChangeResult struct {
	Old map[string]interface{}
	New map[string]interface{}
}

type CreateResult struct {
	New map[string]interface{}
}

type UnknownContent struct {
	S string `json:"msg"`
}

func (e *UnknownContent) Error() string {
	return e.S
}

type JSONError interface {
	Serialize() map[string]interface{} //serialize error to json
}

type ParsedContent map[string]interface{}

//Convert request json data to data and map, you can handle validation here
type MarshalFn func(ctx *gin.Context) (interface{}, error)
type UnMarshalFn func(*gin.Context, [][]byte) (map[string]interface{}, error)

//Get unique key from object and request
type GetKey func(interface{}, *gin.Context) string

//Called when a crud operation is successful
type OnSuccess func(ctx SuccessCtx) (string, error)

//Called when a crud operation fails
type OnError func(ctx interface{}, err error) error

type Results struct {
	Data       []map[string]interface{} `json:"data"`
	Count      int                      `json:"count,omitempty"`
	TotalCount uint64                   `json:"total_count,omitempty"`
}

const (
	FORM_CONTENT = "form"
	JSON_CONTENT = "json"
	XML_CONTENT  = "xml"
)

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Debug(fmt.Sprintf("%s took %s", name, elapsed))
}

func GetFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}

func filterFlags(content string) string {
	for i, a := range content {
		if a == ' ' || a == ';' {
			return content[:i]
		}
	}
	return content
}
func Decode(c *gin.Context, obj interface{}) error {
	ctype := filterFlags(c.Request.Header.Get("Content-Type"))
	switch {
	case c.Request.Method == "GET" || ctype == gin.MIMEPOSTForm:
		return &UnknownContent{"unimplemented content-type: " + ctype}
	case ctype == gin.MIMEJSON:
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(&obj); err == nil {
			return nil
		} else {
			return err
		}
	case ctype == gin.MIMEXML || ctype == gin.MIMEXML2:
		return &UnknownContent{"unimplemented content-type: " + ctype}
	default:
		err := &UnknownContent{"unknown content-type: " + ctype}
		return err
	}
}

func requestContent(c *gin.Context) (ParsedContent, error) {
	ctype := filterFlags(c.Request.Header.Get("Content-Type"))
	switch {
	case c.Request.Method == "GET" || ctype == gin.MIMEPOSTForm:
		return nil, errors.New("Unimplemented content-type: " + ctype)
	case ctype == gin.MIMEJSON:
		var obj ParsedContent
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(obj); err == nil {
			return obj, err
		} else {
			return nil, err
		}
	case ctype == gin.MIMEXML || ctype == gin.MIMEXML2:
		return nil, errors.New("Unimplemented content-type: " + ctype)
	default:
		err := errors.New("unknown content-type: " + ctype)
		c.Fail(400, err)
		return nil, err
	}
}
func doSingleUnmarshal(bucket string, item [][]byte, c *gin.Context, unMarshalFn UnMarshalFn) (data map[string]interface{}, err error) {
	key := string(item[0])
	defer timeTrack(time.Now(), "Do Single Unmarshal "+key+" from "+bucket)
	data, err = unMarshalFn(c, item)
	if err != nil {
		return nil, err
	}
	data["key"] = string(key)
	return
}
func doUnmarshal(key, bucket string, data [][]byte, c *gin.Context, unMarshalFn UnMarshalFn, onSuccess OnSuccess, onError OnError) {

	defer timeTrack(time.Now(), "Do Unmarshal "+key+" from "+bucket)
	m, err := unMarshalFn(c, data)
	if m == nil {
		m = make(map[string]interface{})
	}
	m["key"] = string(data[0])
	if err != nil {
		c.JSON(500, err)
	} else {
		kk := string(data[0])
		if onSuccess != nil {
			ctx := SuccessCtx{Bucket: bucket, Key: kk, Result: m, GinCtx: c}
			onSuccess(ctx)
		}
		c.JSON(200, m)
	}
}
func Get(key, bucket string, store gostore.Store, c *gin.Context, record interface{},
	unMarshalFn UnMarshalFn, onSuccess OnSuccess, onError OnError) {
	data, err := store.Get([]byte(key), bucket)
	if err != nil {
		//TODO: Does not exist error for store
		if onError != nil {
			onError(ErrorCtx{bucket, key, c}, err)
		}
		c.JSON(404, gin.H{"msg": fmt.Sprintf("%s Not found", key)})
	} else {
		if unMarshalFn != nil {
			doUnmarshal(key, bucket, data, c, unMarshalFn, onSuccess, onError)
		} else {
			_ = json.Unmarshal(data[1], record)
			m := structs.Map(record)
			kk := string(data[0])
			m["key"] = kk
			if onSuccess != nil {
				ctx := SuccessCtx{Bucket: bucket, Key: kk, Result: m, GinCtx: c}
				onSuccess(ctx)
			}
			c.JSON(200, m)
		}
	}
}

//TODO: Extract core logic from each crud function i.e make doGetAll, doGet, ... they return data, err
func GetAll(bucket string, store gostore.Store, c *gin.Context, unMarshalFn UnMarshalFn, onSuccess OnSuccess, onError OnError) {
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
		logger.Debug("GetAll", "bucket", bucket)
		data, err = store.GetAll(count+1, 0, bucket)
	}
	if err != nil {
		if onError != nil {
			onError(ErrorCtx{Bucket: bucket, GinCtx: c}, err)
		}
		c.JSON(200, []string{})
	} else {
		if unMarshalFn != nil {
			for _, element := range data {
				data, err := doSingleUnmarshal(bucket, element, c, unMarshalFn)
				if err == nil {
					results = append(results, data)
				}
			}
		} else {
			for _, element := range data {
				var result map[string]interface{}
				if err := json.Unmarshal(element[1], &result); err != nil {
					onError(ErrorCtx{Bucket: bucket, GinCtx: c}, err)
					c.JSON(500, gin.H{"msg": err})
				} else {
					if result == nil {
						result = make(map[string]interface{})
					}

					result["key"] = string(element[0])
					results = append(results, result)
				}
			}
		}
		if len(results) == 0 {
			c.JSON(200, []string{})
		} else {
			if onSuccess != nil {
			}
			stats, _ := store.Stats(bucket)
			total_count := stats["KeyN"].(uint64)
			c.Writer.Header().Set("X-Total-Count", fmt.Sprintf("%d", total_count))
			c.JSON(200, Results{results, count, total_count})
			// c.JSON(200, results)
		}
	}
}

func Post(bucket string, store gostore.Store, c *gin.Context,
	record interface{}, fn GetKey, marshalFn MarshalFn, onSuccess OnSuccess, onError OnError) {

	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1024)
			runtime.Stack(trace, true)
			// fmt.Printf("Stack: %s", trace)
			//				log.Error("Stack of %d bytes: %s", count, trace)
			//				fmt.Println("Defer Panic in auth middleware:", r)
			logger.Error("POST:", "err", string(trace))
			c.JSON(500, gin.H{"message": "Unable to create item "})
			c.Abort()
		}
	}()
	if marshalFn != nil {
		logger.Debug("Post", "bucket", bucket, "marshalfn", GetFunctionName(marshalFn))
		ret, err := marshalFn(c)
		if err != nil {
			if onError != nil {
				onError(ErrorCtx{Bucket: bucket}, err)
			}
			if e, ok := err.(JSONError); ok {
				result := ErrorList{"Malformed data", e.Serialize()}
				c.JSON(400, result)
			} else {
				c.JSON(400, gin.H{"msg": err})
			}

		} else {
			obj := ret.(map[string]interface{})
			key := fn(obj, c)
			if key == "" {
				c.JSON(500, err)
			} else {
				data, err := json.Marshal(obj)
				if err != nil {
					onError(ErrorCtx{Bucket: bucket}, err)
				} else {
					store.Save([]byte(key), data, bucket)
					if onSuccess != nil {

						logger.Debug("onSuccess", "bucket", bucket, "key", key, "onSuccess", GetFunctionName(onSuccess))
						ctx := SuccessCtx{Bucket: bucket, Key: key, Result: obj, GinCtx: c}
						onSuccess(ctx)
					}
					obj["key"] = key
					c.JSON(200, obj)
				}
			}
		}

	} else {
		if b := c.Bind(record); b {
			m := structs.Map(record)
			data, err := json.Marshal(&record)
			key := fn(m, c)
			if err != nil {
				onError(ErrorCtx{Bucket: bucket}, err)
				c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
			} else {
				store.Save([]byte([]byte(key)), data, bucket)
				m["key"] = key
				logger.Debug("Successfully saved object", "bucket", bucket, "key", key)

				if onSuccess != nil {
					ctx := SuccessCtx{Bucket: bucket, Key: key, Result: m, GinCtx: c}
					onSuccess(ctx)
				}
				c.JSON(200, m)
			}
		} else {
			c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly"})
		}
	}
}

func Put(key, bucket string, store gostore.Store, c *gin.Context, record interface{},
	marshalFn MarshalFn, onSuccess OnSuccess, onError OnError) {
	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1024)
			runtime.Stack(trace, true)
			fmt.Printf("Stack: %s", trace)
			//				log.Error("Stack of %d bytes: %s", count, trace)
			//				fmt.Println("Defer Panic in auth middleware:", r)
			logger.Error("Defer Panic in Gincrud PUT:", "err", string(trace))
			c.JSON(500, gin.H{"message": "Unable to edit item "})
			c.Abort()
		}
	}()
	if marshalFn != nil {
		ret, err := marshalFn(c)
		var obj, existing map[string]interface{}
		if change, ok := ret.(ChangeResult); ok {
			obj = change.New
			existing = change.Old
		} else {
			obj = ret.(map[string]interface{})
		}
		if err != nil {
			if onError != nil {
				onError(ErrorCtx{Bucket: bucket}, err)
			}
			if e, ok := err.(JSONError); ok {
				result := ErrorList{"Malformed data", e.Serialize()}
				c.JSON(400, result)
			} else {
				c.JSON(400, gin.H{"msg": err.Error()})
			}

		} else {
			data, err := json.Marshal(obj)
			if err != nil {
				onError(ErrorCtx{Bucket: bucket}, err)
			} else {
				store.Save([]byte([]byte(key)), data, bucket)
				if onSuccess != nil {
					ctx := SuccessCtx{Bucket: bucket, Key: key, Existing: existing, Result: obj, GinCtx: c}
					onSuccess(ctx)
				}
				c.JSON(200, obj)
			}
		}

	} else {
		if b := c.Bind(record); b != false {
			m := structs.Map(record)
			data, err := json.Marshal(&record)
			if err != nil {
				onError(ErrorCtx{Bucket: bucket}, err)
				c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
			} else {
				store.Save([]byte([]byte(key)), data, bucket)
				m["key"] = key

				if onSuccess != nil {
					ctx := SuccessCtx{Bucket: bucket, Key: key, Result: m, GinCtx: c}
					onSuccess(ctx)
				}
				c.JSON(200, m)
			}
		} else {
			c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly"})
		}
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
