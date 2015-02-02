package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bmatsuo/matching-snuggies/slicerjob"
	"github.com/boltdb/bolt"
)

func b(s string) []byte {
	return []byte(s)
}

var DB *bolt.DB

const (
	dbJobs       = "jobs"
	dbMeshFiles  = "meshFiles"
	dbGCodeFiles = "gCodeFiles"
	dbDelFiles   = "deleteFiles"
)

func loadDB(path string) *bolt.DB {
	db, err := bolt.Open(path, 0666, nil)
	if err != nil {
		panic(err)
	}

	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(b(dbDelFiles))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(b(dbMeshFiles))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(b(dbGCodeFiles))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(b(dbJobs))
		if err != nil {
			return err
		}
		return nil
	})
	return db
}

func PutMeshFile(key string, path string) error {
	return DB.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(b(dbMeshFiles)).
			Put(b(key), b(path))
	})
}

func PutGCodeFile(key string, value string) error {
	return DB.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b(dbGCodeFiles))
		if bucket == nil {
			return fmt.Errorf("%v bucket doesn't exist!", dbGCodeFiles)
		}
		return bucket.Put(b(key), b(value))
	})
}

func PutJob(key string, job *slicerjob.Job) error {
	jsonJob, err := json.Marshal(job)
	if err != nil {
		return err
	}

	return DB.Update(func(tx *bolt.Tx) error {
		bucketName := "jobs"
		bucket := tx.Bucket(b(bucketName))
		if bucket == nil {
			return fmt.Errorf("%v bucket doesn't exist!", bucketName)
		}
		return bucket.Put(b(key), jsonJob)
	})
}

func ViewMeshFile(key string) (path string, err error) {
	err = DB.View(func(tx *bolt.Tx) error {
		path = boltGetString(tx, dbMeshFiles, key)
		return nil
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func ViewGCodeFile(key string) (val string, err error) {
	err = DB.View(func(tx *bolt.Tx) error {
		val = string(tx.Bucket(b(dbGCodeFiles)).Get(b(key)))
		return nil
	})
	if err != nil {
		return "", err
	}
	return val, nil
}

func boltCopyKey(tx *bolt.Tx, srcBucket, srcKey, dstBucket, dstKey string) error {
	val := boltGet(tx, srcBucket, srcKey)
	if val == nil {
		return fmt.Errorf("source does not exist")
	}
	return boltPut(tx, dstBucket, dstKey, val)
}

func boltDel(tx *bolt.Tx, bucket, key string) error {
	return tx.Bucket(b(bucket)).Delete(b(key))
}

func boltGet(tx *bolt.Tx, bucket, key string) []byte {
	return tx.Bucket(b(bucket)).Get(b(key))
}

func boltPut(tx *bolt.Tx, bucket, key string, v []byte) error {
	return tx.Bucket(b(bucket)).Put(b(key), v)
}

func boltGetString(tx *bolt.Tx, bucket, key string) string {
	return string(tx.Bucket(b(bucket)).Get(b(key)))
}

func boltPutString(tx *bolt.Tx, bucket, key, v string) error {
	return tx.Bucket(b(bucket)).Put(b(key), b(v))
}

func boltGetJSON(tx *bolt.Tx, bucket, key string, dst interface{}) error {
	js := tx.Bucket(b(bucket)).Get(b(key))
	if len(js) == 0 {
		return fmt.Errorf("not found")
	}
	return json.Unmarshal(js, dst)
}

func boltPutJSON(tx *bolt.Tx, bucket, key string, v interface{}) error {
	js, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return tx.Bucket(b(bucket)).Put(b(key), js)
}

func ViewJob(key string) (*slicerjob.Job, error) {
	var job = new(slicerjob.Job)
	err := DB.View(func(tx *bolt.Tx) error {
		return boltGetJSON(tx, dbJobs, key, job)
	})
	return job, err
}

func viewJob(tx *bolt.Tx, id string) (job *slicerjob.Job) {
	err := boltGetJSON(tx, dbJobs, id, &job)
	if err != nil {
		log.Printf("unmarshal job: %v", err)
		return nil
	}
	return job
}

func CancelJob(id string) error {
	return DB.Update(func(tx *bolt.Tx) error {
		now := time.Now()
		job := viewJob(tx, id)
		if job == nil {
			return fmt.Errorf("job not found")
		}
		job.Status = slicerjob.Cancelled
		job.Terminated = &now
		job.Updated = &now
		return boltPutJSON(tx, dbJobs, id, job)
	})
}

func DeleteJob(id string) error {
	err := DB.Update(func(tx *bolt.Tx) error {
		return deleteJob(tx, id)
	})
	return err
}

func deleteJob(tx *bolt.Tx, id string) error {
	_ = delMeshFile(tx, id)
	_ = delGCodeFile(tx, id)
	return boltDel(tx, dbJobs, id)
}

var ErrMaxDeleted = fmt.Errorf("maximum amount deleted")
var ErrExceededMaxDur = fmt.Errorf("exceeded maximum duration")

func DeleteOldJobs(termBefore time.Time, maxDur time.Duration, maxDel int) error {
	numDel := 0
	var timeout <-chan time.Time
	var istimeout bool
	if maxDur > 0 {
		timeout = time.After(maxDur)
	}
	err := DB.Update(func(tx *bolt.Tx) (err error) {
		curs := tx.Bucket(b(dbJobs)).Cursor()

		for k, v := curs.First(); k != nil; k, v = curs.Next() {
			select {
			case <-timeout:
				istimeout = true
				return nil
			default:
			}
			var job *slicerjob.Job
			err := json.Unmarshal(v, &job)
			if err != nil {
				log.Printf("%q: %v", k, err)
				continue
			}
			if job.Terminated == nil {
				if job.Created == nil {
					log.Printf("job has nil created_time: %v", job.ID)
				} else if termBefore.After(*job.Created) {
					log.Printf("job created %v ago without being terminated: %v", time.Now().Sub(*job.Created), job.ID)
				}
				continue
			}
			if job.Terminated.After(termBefore) {
				continue
			}
			if err := deleteJob(tx, string(k)); err != nil {
				log.Printf("%q: %v", k, err)
				continue
			}
			numDel++
			if numDel >= maxDel {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if numDel > 0 {
		log.Printf("deleted %d jobs", numDel)
	}
	if numDel >= maxDel {
		return ErrMaxDeleted
	}
	if istimeout {
		return ErrExceededMaxDur
	}
	return nil
}

func RemoveFiles(maxDur time.Duration, maxDel int) error {
	numDel := 0
	var timeout <-chan time.Time
	var istimeout bool
	if maxDur > 0 {
		timeout = time.After(maxDur)
	}
	err := DB.Update(func(tx *bolt.Tx) (err error) {
		curs := tx.Bucket(b(dbDelFiles)).Cursor()

		for k, v := curs.First(); k != nil; k, v = curs.Next() {
			select {
			case <-timeout:
				istimeout = true
				return nil
			default:
			}
			path := string(v)
			if err := os.Remove(path); err != nil {
				log.Printf("%q: %v", k, err)
				if !os.IsNotExist(err) {
					continue
				}
			}
			if err := curs.Delete(); err != nil {
				log.Printf("%q: %v", k, err)
				continue
			}
			numDel++
			if numDel >= maxDel {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if numDel > 0 {
		log.Printf("removed %d files", numDel)
	}
	if numDel >= maxDel {
		return ErrMaxDeleted
	}
	if istimeout {
		return ErrExceededMaxDur
	}
	return nil
}

func delMeshFile(tx *bolt.Tx, id string) error {
	err := boltCopyKey(tx,
		dbMeshFiles, id,
		dbDelFiles, fmt.Sprintf("%s/meshes/%s", time.Now().Format(time.RFC3339), id),
	)
	if err != nil {
		return err
	}
	return boltDel(tx, dbGCodeFiles, id)
}

func delGCodeFile(tx *bolt.Tx, id string) error {
	err := boltCopyKey(tx,
		dbGCodeFiles, id,
		dbDelFiles, fmt.Sprintf("%s/gcodes/%s", time.Now().Format(time.RFC3339), id),
	)
	if err != nil {
		return err
	}
	return boltDel(tx, dbGCodeFiles, id)
}
