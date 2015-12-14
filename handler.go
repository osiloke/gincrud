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
	"strings"
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

type InvalidContent struct {
	S string `json:"msg"`
	ContentType string
}

func (e InvalidContent) Error() string {
	return e.S+" content-type:"+e.ContentType
}

type UnknownContent struct {
	S string
}

func (e UnknownContent) Error() string {
	return e.S
}

type JSONError interface {
	Serialize() map[string]interface{} //serialize error to json
}

type ParsedContent map[string]interface{}

//Convert request json data to data and map, you can handle validation here
type MarshalFn func(ctx *gin.Context, opts interface{}) (interface{}, error)
type UnMarshalFn func(*gin.Context, string, map[string]interface{}, interface{}) (map[string]interface{}, error)

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
			return strings.TrimSpace(content[:i])
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
			return &InvalidContent{err.Error(), gin.MIMEJSON}
		}
	case ctype == gin.MIMEXML || ctype == gin.MIMEXML2:
		return &UnknownContent{"unimplemented content-type: " + ctype}
	default:
		return &UnknownContent{"unknown content-type: " + ctype}
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
			return obj, &InvalidContent{err.Error(), gin.MIMEJSON}
		} else {
			return nil, err
		}
	case ctype == gin.MIMEXML || ctype == gin.MIMEXML2:
		return nil, errors.New("Unimplemented content-type: " + ctype)
	default:
		err := errors.New("unknown content-type: " + ctype)
		c.String(400, err.Error())
		return nil, err
	}
}
func doSingleUnmarshal(bucket string, key string, item map[string]interface{}, c *gin.Context, unMarshalFn UnMarshalFn) (data map[string]interface{}, err error) {
	defer timeTrack(time.Now(), "Do Single Unmarshal "+key+" from "+bucket)
	data, err = unMarshalFn(c, key, item, nil)
	if err != nil {
		return nil, err
	}
	data["key"] = string(key)
	return
}
func doUnmarshal(key, bucket string, data map[string]interface{}, c *gin.Context, unMarshalFn UnMarshalFn, onSuccess OnSuccess, onError OnError) {

	defer timeTrack(time.Now(), "Do Unmarshal "+key+" from "+bucket)
	m, err := unMarshalFn(c, key, data, nil)
	if m == nil {
		m = make(map[string]interface{})
	}
	m["key"] = key
	if err != nil {
		c.JSON(500, err)
	} else {

		if onSuccess != nil {
			ctx := SuccessCtx{Bucket: bucket, Key: key, Result: m, GinCtx: c}
			onSuccess(ctx)
		}
		c.JSON(200, m)
	}
}
func Get(key, bucket string, store gostore.ObjectStore, c *gin.Context, record interface{},
	unMarshalFn UnMarshalFn, onSuccess OnSuccess, onError OnError) {
	var data map[string]interface{}
	err := store.Get(key, bucket, &data)
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
//			_ = json.Unmarshal(data[1], record)
//			m := structs.Map(record)
			record = data
			if onSuccess != nil {
				ctx := SuccessCtx{Bucket: bucket, Key: key, Result: data, GinCtx: c}
				onSuccess(ctx)
			}
			c.JSON(200, data)
		}
	}
}

//TODO: Extract core logic from each crud function i.e make doGetAll, doGet, ... they return data, err
func GetAll(bucket string, store gostore.ObjectStore, c *gin.Context, unMarshalFn UnMarshalFn, onSuccess OnSuccess, onError OnError) {
	var results []map[string]interface{}
	var err error

	count := 10
	q := c.Request.URL.Query()
	if val, ok := q["_perPage"]; ok {
		count, _ = strconv.Atoi(val[0])
	}
	var rows gostore.ObjectRows
	if val, ok := q["afterKey"]; ok {
		rows, err = store.Before(val[0], count+1, 0, bucket)
	} else if val, ok := q["beforeKey"]; ok {
		rows, err = store.Since(val[0], count+1, 0, bucket)
	} else {
		logger.Debug("GetAll", "bucket", bucket)
		rows, err = store.All(count+1, 0, bucket)
	}
	if err != nil {
		if onError != nil {
			onError(ErrorCtx{Bucket: bucket, GinCtx: c}, err)
		}
		c.JSON(200, []string{})
	} else {
		if unMarshalFn != nil {
			var ok bool = true
			for ok{
				var data interface{}
				if ok, err := rows.Next(&data); ok {
					element := data.(map[string]interface{})
					marshalled_element, err := doSingleUnmarshal(bucket, element["id"].(string), element, c, unMarshalFn)
					if err == nil {
						results = append(results, marshalled_element)
					}
				}else{
					if err != nil{
						logger.Debug("Error while retrieving a row", "err", err)
					}
					break
				}
			}
		} else {
			var ok bool = true
			for ok{
				var data interface{}
				if ok, err := rows.Next(&data); ok {
					results = append(results, data.(map[string]interface{}))
				}else{
					if err != nil{
						logger.Debug("Error while retrieving a row", "err", err)
					}
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

func Post(bucket string, store gostore.ObjectStore, c *gin.Context,
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
		ret, err := marshalFn(c, nil)
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
				obj["id"] = key
				if _, err := store.Save(bucket, obj); err == nil {
					if onSuccess != nil {

						logger.Debug("onSuccess", "bucket", bucket, "key", key, "onSuccess", GetFunctionName(onSuccess))
						ctx := SuccessCtx{Bucket: bucket, Key: key, Result: obj, GinCtx: c}
						onSuccess(ctx)
					}
					obj["key"] = key
					c.JSON(200, obj)
					return
				}else{
					onError(ErrorCtx{Bucket: bucket}, err)
					c.JSON(500, gin.H{"msg": err.Error()})
				}
			}
		}

	} else {
		if b := c.Bind(record); b == nil {
			m := structs.Map(record)
			key := fn(m, c)
			m["id"] = key
			if _, err := store.Save(bucket, m); err == nil {
				logger.Debug("Successfully saved object", "bucket", bucket, "key", key)

				if onSuccess != nil {
					ctx := SuccessCtx{Bucket: bucket, Key: key, Result: m, GinCtx: c}
					onSuccess(ctx)
				}
				c.JSON(200, m)
			}else{
				onError(ErrorCtx{Bucket: bucket}, err)
				c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
			}
		} else {
			c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly", "because": b.Error()})
		}
	}
}

func Put(key, bucket string, store gostore.ObjectStore, c *gin.Context, record interface{},
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
		ret, err := marshalFn(c, nil)
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
			if err := store.Update(key, bucket, obj); err == nil {
				if onSuccess != nil {
					ctx := SuccessCtx{Bucket: bucket, Key: key, Existing: existing, Result: obj, GinCtx: c}
					onSuccess(ctx)
				}
				c.JSON(200, obj)
			}else{
				if onError != nil{
					onError(ErrorCtx{Bucket: bucket}, err)
				}
				c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
			}
		}

	} else {
		if b := c.Bind(record); b == nil {
			m := structs.Map(record)
			if err := store.Update(key, bucket, m); err == nil {
				m["key"] = key
				if onSuccess != nil {
					ctx := SuccessCtx{Bucket: bucket, Key: key, Result: m, GinCtx: c}
					onSuccess(ctx)
				}
				c.JSON(200, m)
			}else{
				if onError != nil{
					onError(ErrorCtx{Bucket: bucket}, err)
				}
				c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
			}
		} else {
			c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly", "because": b.Error()})
		}
	}
}

func Delete(key, bucket string, store gostore.ObjectStore, c *gin.Context, onSuccess OnSuccess, onError OnError) {
	err := store.Delete(key, bucket)
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
