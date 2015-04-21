package llsr

import (
	"database/sql"
	_ "github.com/lib/pq"
	"os"
	"testing"
	"time"
)

type testConnCallback func(*testing.T, *sql.DB)
type testStreamCallback func(*testing.T, *sql.DB, *Stream)

var user string
var dbname string

func dbUser() string {
	if len(user) == 0 {
		user = os.Getenv("PG_LLSR_TEST_USER")
		if len(user) == 0 {
			user = "postgres"
		}
	}
	return user
}

func dbName() string {
	if len(dbname) == 0 {
		dbname = os.Getenv("PG_LLSR_TEST_DB")
		if len(dbname) == 0 {
			dbname = "postgres"
		}
	}
	return dbname
}

func withTestConnection(t *testing.T, cb testConnCallback) {
	db, err := sql.Open("postgres", "sslmode=disable user="+dbUser()+" dbname="+dbName())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE llsr_test_table (id int primary key, txt text NOT NULL);")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE llsr_test_table")

	_, err = db.Exec("SELECT * FROM pg_create_logical_replication_slot('llsr_test_slot', 'decoderbufs')")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("SELECT * FROM pg_drop_replication_slot('llsr_test_slot')")

	cb(t, db)
}

func TestStreamOpeningAndClosing(t *testing.T) {
	withTestConnection(t, func(t *testing.T, db *sql.DB) {
		config := NewDatabaseConfig(dbName())
		config.User = dbUser()

		stream := NewStream(config, "llsr_test_slot", 0)

		err := stream.Start()
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(1000000000)

		stream.Stop()
		err = <-stream.Finished()
		if err != nil {
			t.Fatal(err)
		}
	})
}

func withTestStream(t *testing.T, cb testStreamCallback) {
	withTestConnection(t, func(t *testing.T, db *sql.DB) {
		config := NewDatabaseConfig(dbName())
		config.User = dbUser()

		stream := NewStream(config, "llsr_test_slot", 0)

		err := stream.Start()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			stream.Stop()
			err = <-stream.Finished()
			if err != nil {
				t.Fatal(err)
			}
		}()

		time.Sleep(1000000000)

		cb(t, db, stream)
	})
}

func TestStreamMessages(t *testing.T) {
	withTestStream(t, func(t *testing.T, db *sql.DB, stream *Stream) {
		_, err := db.Exec("INSERT INTO llsr_test_table(id, txt) VALUES (1, 'value_1') ")
		if err != nil {
			t.Fatal(err)
		}

		_, err = db.Exec("UPDATE llsr_test_table SET txt = 'value_2' WHERE id = 1")
		if err != nil {
			t.Fatal(err)
		}

		<-stream.Data()
		msg2 := <-stream.Data()

		if msg2.GetTable() != "llsr_test_table" {
			t.Fatal("Expected change in llsr_test_table")
		}

		op := msg2.GetOp()

		if op != Op_UPDATE {
			t.Fatalf("Expected UPDATE change got %s", Op_name[int32(op)])
		}

	})
}

func TestUnexpectedIOBehaviour(t *testing.T) {
	withTestConnection(t, func(t *testing.T, db *sql.DB) {
		config := NewDatabaseConfig(dbName())
		config.User = dbUser()

		stream := NewStream(config, "llsr_test_slot", 0)

		err := stream.Start()
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(1000000000)

		stream.stdOut.Close()

		_, err = db.Exec("INSERT INTO llsr_test_table(id, txt) VALUES (1, 'value_1') ")
		if err != nil {
			t.Fatal(err)
		}

		select {
		case err = <-stream.Finished():
			if err == nil {
				t.Fatalf("Expected Finished() to return error")
			}
		case <-time.Tick(5 * time.Second):
			t.Fatal("Timeout")
		}
	})
}

func TestXLogPosConversion(t *testing.T) {
  conversions := make(map[string]LogPos)
  conversions["0/243C4C60"] = LogPos(607931488)
  conversions["A1/243C4C60"] = LogPos(692097666144)
  
  for k, v := range conversions {
    rk := v.ToString()
    if rk != k {
      t.Fatalf("expected LogPos(%d).ToString() to equal %s; got %v", v, k, rk)
    }
  }
}
