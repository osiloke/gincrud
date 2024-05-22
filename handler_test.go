package gincrud

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecode(t *testing.T) {
	body := new(bytes.Buffer)
	multipartWriter := multipart.NewWriter(body)
	//Create multipart header
	fileHeader := make(textproto.MIMEHeader)
	fileHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "file", "sample_success.csv"))
	fileHeader.Set("Content-Type", "text/plain")
	multipartWriter.CreatePart(fileHeader)
	name, _ := multipartWriter.CreateFormField("name")
	name.Write([]byte("value"))
	multipartWriter.Close()
	request := httptest.NewRequest(http.MethodPost, "/content/import", body)
	request.Header.Add("Content-Type", multipartWriter.FormDataContentType())

	type args struct {
		c   *gin.Context
		obj map[string]interface{}
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
						Body: io.NopCloser(strings.NewReader(`{"hello": "world"}`)),
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
			"test multipart/form-data",
			args{
				&gin.Context{
					Request: request,
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
						Body: io.NopCloser(strings.NewReader(`{"hello": "world"}`)),
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
			if err := Decode(tt.args.c, &tt.args.obj); (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
