package db

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ResultsLimit = 1000
)

var pg *pgxpool.Pool

func Close() {
	pg.Close()
}

func Init(ctx context.Context) error {
	if pg != nil {
		return nil
	}

	pool, err := pgxpool.New(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping db: %w", err)
	}

	pg = pool

	return nil
}

func Shutdown() {
	pg.Close()
}

func where(v any) (string, []any) {
	var conds []string
	var params []any

	value := reflect.ValueOf(v)
	if value.IsNil() {
		return "", nil
	}

	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}

	t := value.Type()
	for i := range t.NumField() {
		sf := t.Field(i)
		tag := sf.Tag.Get("condition")

		vf := value.FieldByName(sf.Name)
		if vf.IsZero() {
			continue
		}

		var typ string
		vfi := vf.Interface()

		op := "="
		column := tag
		idx := strings.LastIndex(column, ",")
		if idx != -1 {
			op = tag[idx+1:]
			column = tag[:idx]
		}

		switch vf.Kind() {
		case reflect.Bool:
			typ = "boolean"

		case reflect.Int64:
			typ = "bigint"

		case reflect.Int32, reflect.Int:
			typ = "integer"

		case reflect.Int16, reflect.Int8:
			typ = "smallint"

		case reflect.String:
			typ = "text"

		case reflect.Struct:
			switch vfi.(type) {
			case time.Time:
				typ = "timestamp"
			}
		}

		if typ == "" {
			panic("couldn't figure out type")
		}

		conds = append(conds, fmt.Sprintf("%s %s $%d::%s", column, op, len(conds)+1, typ))
		params = append(params, vfi)
	}

	return strings.Join(conds, " and "), params
}
