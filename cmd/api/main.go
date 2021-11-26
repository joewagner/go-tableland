package main

import (
	"context"
	"net/http"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/textileio/go-tableland/internal/tableland"
	"github.com/textileio/go-tableland/internal/tableland/impl"
	sqlstoreimpl "github.com/textileio/go-tableland/pkg/sqlstore/impl"
)

func main() {
	config := setupConfig()

	server := rpc.NewServer()

	ctx := context.Background()
	name, svc := getTablelandService(ctx, config)
	server.RegisterName(name, svc)

	http.HandleFunc("/rpc", func(rw http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(rw, r)
	})

	err := http.ListenAndServe(":"+config.HTTP.Port, nil)
	if err != nil {
		panic(err)
	}
}

func getTablelandService(ctx context.Context, conf *config) (string, tableland.Tableland) {
	switch conf.Impl {
	case "mesa":
		sqlstore, err := sqlstoreimpl.NewPostgres(ctx, conf.DB.Host, conf.DB.Port, conf.DB.User, conf.DB.Pass, conf.DB.Name)
		if err != nil {
			panic(err)
		}
		return tableland.ServiceName, &impl.TablelandMesa{sqlstore, nil}

	case "mock":
		return tableland.ServiceName, new(impl.TablelandMock)

	}
	return tableland.ServiceName, new(impl.TablelandMock)
}
