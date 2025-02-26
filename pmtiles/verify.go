package pmtiles

import (
	"bytes"
	"context"
	"fmt"
	"github.com/RoaringBitmap/roaring/roaring64"
	"io"
	"log"
	"math"
	"time"
)

func Verify(logger *log.Logger, file string) error {
	start := time.Now()
	ctx := context.Background()

	bucketURL, key, err := NormalizeBucketKey("", "", file)

	if err != nil {
		return err
	}

	bucket, err := OpenBucket(ctx, bucketURL, "")

	if err != nil {
		return fmt.Errorf("Failed to open bucket for %s, %w", bucketURL, err)
	}
	defer bucket.Close()

	r, err := bucket.NewRangeReader(ctx, key, 0, 16384)

	if err != nil {
		return fmt.Errorf("Failed to create range reader for %s, %w", key, err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("Failed to read %s, %w", key, err)
	}
	r.Close()

	header, err := deserializeHeader(b[0:HeaderV3LenBytes])

	var CollectEntries func(uint64, uint64, func(EntryV3))

	CollectEntries = func(dir_offset uint64, dir_length uint64, f func(EntryV3)) {
		dirbytes, err := bucket.NewRangeReader(ctx, key, int64(dir_offset), int64(dir_length))
		if err != nil {
			panic(fmt.Errorf("I/O error"))
		}
		defer dirbytes.Close()
		b, err = io.ReadAll(dirbytes)
		if err != nil {
			panic(fmt.Errorf("I/O Error"))
		}

		directory := deserializeEntries(bytes.NewBuffer(b))
		for _, entry := range directory {
			if entry.RunLength > 0 {
				f(entry)
			} else {
				CollectEntries(header.LeafDirectoryOffset+entry.Offset, uint64(entry.Length), f)
			}
		}
	}

	var minTileID uint64
	var maxTileID uint64
	minTileID = math.MaxUint64
	maxTileID = 0

	addressedTiles := 0
	tileEntries := 0
	offsets := roaring64.New()
	var currentOffset uint64
	CollectEntries(header.RootOffset, header.RootLength, func(e EntryV3) {
		offsets.Add(e.Offset)
		addressedTiles += int(e.RunLength)
		tileEntries++

		if e.TileID < minTileID {
			minTileID = e.TileID
		}
		if e.TileID > maxTileID {
			maxTileID = e.TileID
		}

		if e.Offset+uint64(e.Length) > header.TileDataLength {
			fmt.Printf("Invalid: %v outside of tile data section", e)
		}

		if header.Clustered {
			if !offsets.Contains(e.Offset) {
				if e.Offset != currentOffset {
					fmt.Printf("Invalid: out-of-order entry %v in clustered archive.", e)
				}
				currentOffset += uint64(e.Length)
			}
		}
	})

	if uint64(addressedTiles) != header.AddressedTilesCount {
		fmt.Printf("Invalid: header AddressedTilesCount=%v but %v tiles addressed.", header.AddressedTilesCount, addressedTiles)
	}

	if uint64(tileEntries) != header.TileEntriesCount {
		fmt.Printf("Invalid: header TileEntriesCount=%v but %v tile entries.", header.TileEntriesCount, tileEntries)
	}

	if offsets.GetCardinality() != header.TileContentsCount {
		fmt.Printf("Invalid: header TileContentsCount=%v but %v tile contents.", header.TileContentsCount, offsets.GetCardinality())
	}

	if z, _, _ := IDToZxy(minTileID); z != header.MinZoom {
		fmt.Printf("Invalid: header MinZoom=%v does not match min tile z %v", header.MinZoom, z)
	}

	if z, _, _ := IDToZxy(maxTileID); z != header.MaxZoom {
		fmt.Printf("Invalid: header MaxZoom=%v does not match max tile z %v", header.MaxZoom, z)
	}

	if !(header.CenterZoom >= header.MinZoom && header.CenterZoom <= header.MaxZoom) {
		fmt.Printf("Invalid: header CenterZoom=%v not within MinZoom/MaxZoom.", header.CenterZoom)
	}

	fmt.Printf("Completed verify in %v.\n", time.Since(start))
	return nil
}
