package storage

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"

	"cloud.google.com/go/storage"
)

var client *storage.Client
var bkt *storage.BucketHandle

func Init(ctx context.Context, bucket string) error {
	c, err := storage.NewGRPCClient(ctx)
	if err != nil {
		return err
	}

	bkt = c.Bucket(bucket)
	client = c

	return err
}

func NewMailboxMessageReader(ctx context.Context, domain, acctId, mboxId string, uid uint32) (io.ReadCloser, error) {
	name := objectName(domain, acctId, mboxId, uid)
	return bkt.Object(name).NewReader(ctx)
}

func WriteMailboxMessage(ctx context.Context, domain, acctId, mboxId string, uid uint32, data []byte) (int, error) {
	name := objectName(domain, acctId, mboxId, uid)
	w := bkt.Object(name).NewWriter(ctx)
	w.ContentType = "application/octet-stream"
	w.CRC32C = crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
	w.SendCRC32C = true

	size, err := w.Write(data)
	if err != nil {
		return 0, err
	}

	return size, w.Close()
}

func objectName(domain, acctId, mboxId string, uid uint32) string {
	return fmt.Sprintf("%s/%s/%s/%d.eml", domain, acctId, mboxId, uid)
}
