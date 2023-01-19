package gincrud

import (
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecode(t *testing.T) {
	type args struct {
		c   *gin.Context
		obj interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{

		{
			"test custom application/vid.api+json",
			args{
				&gin.Context{
					Request: &http.Request{
						Body: ioutil.NopCloser(strings.NewReader(`{"hello": "world"}`)),
						Header: http.Header{
							"Content-Type": {"application/vid.api+json"},
						},
					},
				},
				map[string]interface{}{},
			},
			false,
		},
		{
			"test unknown mime type",
			args{
				&gin.Context{
					Request: &http.Request{
						Body: ioutil.NopCloser(strings.NewReader(`{"hello": "world"}`)),
						Header: http.Header{
							"Content-Type": {"application/what-the-hell"},
						},
					},
				},
				map[string]interface{}{},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Decode(tt.args.c, tt.args.obj); (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
