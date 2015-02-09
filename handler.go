package gincrud

import (
	"fmt"
	"encoding/json"
	"github.com/fatih/structs"
	"github.com/gin-gonic/gin"
	"github.com/osiloke/gostore"
)

type GetKey func(interface{}, *gin.Context) string

func GetAll(bucket string, store gostore.Store, c *gin.Context) {
	var results []map[string]interface{}
	data, _ := store.GetAll(10, bucket)
	for _, element := range data {
		var result map[string]interface{}
		_ = json.Unmarshal(element[1], &result)
		result["key"] = string(element[0])
		results = append(results, result)
	}
	if len(results) == 0 {
		//		c.JSON(204, nil)
		c.Abort(204)
	} else {
		c.JSON(200, results)
	}
}

func Get(key, bucket string, store gostore.Store, c *gin.Context, record interface{}) {
	data, err := store.Get([]byte(key), bucket)
	if err != nil {
		//TODO: Does not exist error for store
		c.JSON(404, gin.H{"msg": fmt.Sprintf("An error occured while retrieving %s - %s", key, err)})
	} else {
		_ = json.Unmarshal(data[1], record)
		m := structs.Map(record)
		m["key"] = string(data[0])
		c.JSON(200, m)
	}
}

func Post(bucket string, store gostore.Store, c *gin.Context, record interface{}, fn GetKey) {
	if b := c.Bind(record); b != false {
		m := structs.Map(record)
		data, err := json.Marshal(&record)
		key := fn(m, c)
		if err != nil {
			c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
		} else {
			store.Save([]byte([]byte(key)), data, bucket)
			m["key"] = key
			c.JSON(200, m)
		}
	} else {
		c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly"})
	}
}

func Put(key, bucket string, store gostore.Store, c *gin.Context, record interface{}) {
	if b := c.Bind(record); b != false {
		data, err := json.Marshal(&record)
		if err != nil {
			c.JSON(500, gin.H{"msg": "An error occured and this item could not be saved"})
		} else {
			store.Save([]byte([]byte(key)), data, bucket)
			c.JSON(200, structs.Map(record))
		}
	} else {
		c.JSON(400, gin.H{"msg": "Seems like the data submitted is not formatted properly"})
	}
}

func Delete(key, bucket string, store gostore.Store, c *gin.Context) {
	err := store.Delete([]byte(key), bucket)
	if err != nil {
		c.JSON(500, gin.H{"msg": "The item [" + key + "] was not deleted"})
	} else {
		c.JSON(200, gin.H{"msg": "The item [" + key + "] was deleted"})
	}
}
