# Fetching remote data using HTTP requests with transparent retries.

## Installation

```
go get github.com/stockparfait/fetcrh
```

## Example usage

```go
package main

import (
	"context"
	"fmt"

	"github.com/stockparfait/fetch"
)

func main() {
	ctx := context.Background()
	params := fetch.NewParams().Retries(2)
	r, err := fetch.GetRetry(ctx, "https://google.com/", nil, params)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Printf("Response status: %s\n", r.Status)
}
```

This should print:

```
Response status: 200 OK
```

## Development

Clone and initialize the repository, run tests:

```sh
git clone git@github.com:stockparfait/fetch.git
cd fetch
make init
make test
```
