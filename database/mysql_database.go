package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	mysql_createTableQuery    = "CREATE TABLE IF NOT EXISTS %v (id VARCHAR(14) NOT NULL, data json NOT NULL, PRIMARY KEY (id)) ENGINE=InnoDB;"
	mysql_dropNamespaceQuery  = "DROP TABLE %v"
	mysql_insertQuery         = "INSERT INTO %v (id, data) VALUES(?, ?) ON DUPLICATE KEY UPDATE data = ?"
	mysql_tablesQuery         = "SELECT table_name FROM information_schema.tables WHERE table_schema = '%v'"
	mysql_recordExistentQuery = "SELECT COUNT(1) FROM %v WHERE id = ?"
	mysql_getQuery            = "SELECT data FROM %v WHERE id = ?"
	mysql_getAllQuery         = "SELECT id, data FROM %v ORDER BY id"
	mysql_deleteQuery         = "DELETE FROM %v WHERE id = ?"
	mysql_deleteAllQuery      = "TRUNCATE TABLE %v"
	mysql_dbTimeout           = 10 * time.Second
)

type MySqlDatabase struct {
	Host string
	Name string
	User string
	Pass string

	db *sql.DB
}

func (m *MySqlDatabase) Init() {
	connInfo := fmt.Sprintf("%v:%v@tcp(%v)/%v", m.User, m.Pass, m.Host, m.Name)
	db, err := sql.Open("mysql", connInfo)

	if err != nil {
		log.Println(connInfo)
		log.Fatalf("error connecting to mysql: %v", err)
	}
	db.SetConnMaxLifetime(time.Hour * 1)
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(10)

	m.db = db
	log.Println("db connected")
}

func (m *MySqlDatabase) Disconnect() {
	err := m.db.Close()
	if err != nil {
		panic(err)
	}
	log.Println("diconnected")
}

func (m *MySqlDatabase) CreateNameSpace(namespace string) *DbError {
	err := m.ensureNamespace(namespace)
	if err != nil {
		return &DbError{
			ErrorCode: NAMESPACE_NOT_FOUND,
			Message:   fmt.Sprintf("could not create namespace %v", namespace),
		}
	}
	return nil
}

func (m *MySqlDatabase) GetNamespaces() []string {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	sqlStatement := fmt.Sprintf(mysql_tablesQuery, m.Name)
	rows, err := m.db.QueryContext(ctx, sqlStatement)
	if err != nil {
		log.Printf("error on GetNamespaces: %v\n", err)
	}
	defer rows.Close()

	ret := make([]string, 0)
	for rows.Next() {
		var tableName string
		err = rows.Scan(&tableName)
		if err != nil {
			log.Println(sqlStatement)
			log.Printf("error on Scan: %v\n", err)
		}
		ret = append(ret, tableName)
	}
	return ret
}

func (m *MySqlDatabase) DropNameSpace(namespace string) *DbError {
	return nil
}

func (m *MySqlDatabase) Upsert(namespace string, key string, value []byte, allowOverWrite bool) *DbError {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	err := m.ensureNamespace(namespace)

	if err != nil {
		return &DbError{
			ErrorCode: NAMESPACE_NOT_FOUND,
			Message:   fmt.Sprintf("namespace %v does not exist", namespace),
		}
	}

	if !allowOverWrite {
		res := m.db.QueryRowContext(ctx, fmt.Sprintf(mysql_recordExistentQuery, namespace), key)
		var count string
		err := res.Scan(&count)
		if err != nil {
			return &DbError{
				ErrorCode: INTERNAL_ERROR,
				Message:   fmt.Sprintf("error on QueryRow: %v", err),
			}
		}
		if count == "1" {
			return &DbError{
				ErrorCode: ITEM_CONFLICT,
				Message:   "item already exists",
			}
		}
	}

	_, dbErr := m.db.ExecContext(ctx, fmt.Sprintf(mysql_insertQuery, namespace), key, string(value), string(value))
	if dbErr != nil {
		return &DbError{
			ErrorCode: INTERNAL_ERROR,
			Message:   fmt.Sprintf("error on Upsert: %v", dbErr),
		}
	}
	return nil
}

func (m *MySqlDatabase) Get(namespace string, key string) ([]byte, *DbError) {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	rows, dbErr := m.db.QueryContext(ctx, fmt.Sprintf(mysql_getQuery, namespace), key)
	if dbErr != nil {
		return nil, &DbError{
			ErrorCode: INTERNAL_ERROR,
			Message:   fmt.Sprintf("error on Get: %v", dbErr),
		}
	}
	defer rows.Close()
	if rows.Next() {
		var data string
		scanErr := rows.Scan(&data)
		if scanErr != nil {
			return nil, &DbError{
				ErrorCode: INTERNAL_ERROR,
				Message:   fmt.Sprintf("scan %v", scanErr),
			}
		}
		return []byte(data), nil
	}
	return nil, &DbError{
		ErrorCode: ID_NOT_FOUND,
		Message:   fmt.Sprintf("value not found in namespace %v for key %v", namespace, key),
	}
}

func (m *MySqlDatabase) GetAll(namespace string) (map[string][]byte, *DbError) {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	sqlStatement := fmt.Sprintf(mysql_getAllQuery, namespace)
	rows, dbErr := m.db.QueryContext(ctx, sqlStatement)
	if dbErr != nil {
		return nil, &DbError{
			ErrorCode: INTERNAL_ERROR,
			Message:   fmt.Sprintf("error on Get: %v", dbErr),
		}
	}
	defer rows.Close()

	ret := make(map[string][]byte)

	for rows.Next() {
		var id, data string
		scanErr := rows.Scan(&id, &data)
		if scanErr != nil {
			return nil, &DbError{
				ErrorCode: INTERNAL_ERROR,
				Message:   fmt.Sprintf("scan %v", scanErr),
			}
		}
		ret[id] = []byte(data)
	}
	return ret, nil
}

func (m *MySqlDatabase) Delete(namespace string, key string) *DbError {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	sqlStatement := fmt.Sprintf(mysql_deleteQuery, namespace)
	_, err := m.db.ExecContext(ctx, sqlStatement, key)
	if err != nil {
		log.Println(sqlStatement)
		message := fmt.Sprintf("error on Delete: %v", err)
		return &DbError{
			ErrorCode: INTERNAL_ERROR,
			Message:   message,
		}
	}
	return nil
}

func (m *MySqlDatabase) DeleteAll(namespace string) *DbError {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	sqlStatement := fmt.Sprintf(mysql_deleteAllQuery, namespace)
	_, err := m.db.ExecContext(ctx, sqlStatement)
	if err != nil {
		log.Println(sqlStatement)
		message := fmt.Sprintf("error on DeleteAll: %v", err)
		return &DbError{
			ErrorCode: INTERNAL_ERROR,
			Message:   message,
		}
	}
	return nil
}

func (m *MySqlDatabase) ensureNamespace(namespace string) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), mysql_dbTimeout)
	defer cancel()
	query := fmt.Sprintf(mysql_createTableQuery, namespace)
	_, err = m.db.ExecContext(ctx, query)

	if err != nil {
		log.Println(query)
		log.Printf("error creating table: %v\n", err)
	}

	return err
}
