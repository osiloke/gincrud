package gincrud

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fatih/structs"
	"github.com/gin-gonic/gin"
	"github.com/osiloke/gostore"
	"strconv"
)

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
	Bucket string
	Key    string
	Result map[string]interface{}
	GinCtx *gin.Context
}

type MarshalError struct {
	Data map[string]interface{}
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
type MarshalFn func(ctx *gin.Context) (map[string]interface{}, error)
type UnMarshalFn func(*gin.Context, []byte) (map[string]interface{}, error)

//Get unique key from object and request
type GetKey func(interface{}, *gin.Context) string

//Called when a crud operation is successful
type OnSuccess func(ctx SuccessCtx) (string, error)

//Called when a crud operation fails
type OnError func(ctx interface{}, err error) error

type Results struct {
	Data       []map[string]interface{} `json:"data"`
	Count      int                      `json:"count,omitempty"`
	TotalCount int                      `json:"total_count,omitempty"`
}

const (
	FORM_CONTENT = "form"
	JSON_CONTENT = "json"
	XML_CONTENT  = "xml"
)

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
			total_count := stats["KeyN"].(int)
			c.Writer.Header().Set("X-Total-Count", fmt.Sprintf("%d", total_count))
			c.JSON(200, Results{results, count, total_count})
			// c.JSON(200, results)
		}
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
		fmt.Println("Could not retrieve", key, "from", bucket)
		c.JSON(404, gin.H{"msg": fmt.Sprintf("Not found")})
	} else {
		if unMarshalFn != nil {
			m, err := unMarshalFn(c, data[1])
			m["key"] = string(data[0])
			if err != nil {
				c.JSON(500, err)
			} else {
				kk := string(data[0])
				if onSuccess != nil {
					ctx := SuccessCtx{bucket, kk, m, c}
					onSuccess(ctx)
				}
				c.JSON(200, m)
			}
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
}

func Post(bucket string, store gostore.Store, c *gin.Context,
	record interface{}, fn GetKey, marshalFn MarshalFn, onSuccess OnSuccess, onError OnError) {
	if marshalFn != nil {
		obj, err := marshalFn(c)
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
			key := fn(obj, c)
			if key == "" {
				c.JSON(500, err)
			} else {
				data, err := json.Marshal(obj)
				if err != nil {
					onError(ErrorCtx{Bucket: bucket}, err)
				} else {
					store.Save([]byte([]byte(key)), data, bucket)
					if onSuccess != nil {
						ctx := SuccessCtx{bucket, key, obj, c}
						onSuccess(ctx)
					}
					obj["key"] = key
					c.JSON(200, obj)
				}
			}
		}

	} else {
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
}

func Put(key, bucket string, store gostore.Store, c *gin.Context, record interface{},
	marshalFn MarshalFn, onSuccess OnSuccess, onError OnError) {
	if marshalFn != nil {
		obj, err := marshalFn(c)
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
					ctx := SuccessCtx{bucket, key, obj, c}
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
					ctx := SuccessCtx{bucket, key, m, c}
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
