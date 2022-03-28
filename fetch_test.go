// Copyright 2022 Stock Parfait

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fetch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFetch(t *testing.T) {
	t.Parallel()
	Convey("fetch package works", t, func() {
		requestQuery := url.Values{}
		requestQuery.Set("k1", "v1")
		requestQuery.Set("k2", "v2")
		testParams := NewParams().MinWait(time.Millisecond).MaxWait(10 * time.Millisecond)

		server := NewTestServer()
		server.ResponseBody = []string{"Test response"}
		defer server.Close()
		ctx := UseClient(context.Background(), server.Client())

		Convey("Get handles a response", func() {
			r, err := Get(ctx, server.URL(), requestQuery)
			So(err, ShouldBeNil)
			var respBody = make([]byte, 200)
			n, err := r.Body.Read(respBody)
			So(n, ShouldEqual, len(server.ResponseBody[0]))
			respBody = respBody[:n]
			So(err, ShouldEqual, io.EOF)
			So(string(respBody), ShouldResemble, server.ResponseBody[0])
			So(server.RequestQuery, ShouldResemble, requestQuery)
		})

		Convey("GetRetry works on the first try with default params", func() {
			r, err := GetRetry(ctx, server.URL(), requestQuery, nil)
			So(err, ShouldBeNil)
			So(r.StatusCode, ShouldEqual, http.StatusOK)
		})

		Convey("GetRetry retries once", func() {
			server.ResponseStatus = []int{http.StatusInternalServerError, http.StatusOK}
			r, err := GetRetry(ctx, server.URL(), requestQuery, testParams)
			So(err, ShouldBeNil)
			So(r.StatusCode, ShouldEqual, http.StatusOK)
		})

		Convey("GetRetry should not retry permanent failure", func() {
			server.ResponseStatus = []int{http.StatusForbidden, http.StatusOK}
			r, err := GetRetry(ctx, server.URL(), requestQuery, testParams)
			So(err, ShouldNotBeNil)
			So(r.StatusCode, ShouldEqual, http.StatusForbidden)
		})

		Convey("FetchJSON successfully decodes a struct", func() {
			type FieldType struct {
				Field string `json:"field"`
			}
			var f FieldType
			server.ResponseBody = []string{`{"field": "test value"}`}
			err := FetchJSON(ctx, server.URL(), &f, nil, nil)
			So(err, ShouldBeNil)
			So(f.Field, ShouldEqual, "test value")
		})

	})
}
