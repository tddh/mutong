package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9669", "nebula graphd addr")
	user := flag.String("user", "root", "nebula user")
	pass := flag.String("pass", "nebula", "nebula password")
	space := flag.String("space", "mutong", "nebula space")
	dryRun := flag.Bool("dry-run", false, "only print merges, do not execute")
	flag.Parse()

	host, port := parseAddr(*addr)
	pool, err := nebula.NewConnectionPool([]nebula.HostAddress{{Host: host, Port: port}}, nebula.GetDefaultConf(), nebula.DefaultLogger{})
	if err != nil {
		log.Fatalf("connect nebula: %v", err)
	}
	defer pool.Close()

	sess, err := pool.GetSession(*user, *pass)
	if err != nil {
		log.Fatalf("nebula session: %v", err)
	}
	defer sess.Release()

	if _, err := sess.Execute("USE " + *space); err != nil {
		log.Fatalf("use space %s: %v", *space, err)
	}

	merged := 0
	query := `MATCH (v:BusinessApp) WHERE v.BusinessApp.owner_name != "" AND v.BusinessApp.app_name != v.BusinessApp.owner_name RETURN v.BusinessApp.uid AS uid, v.BusinessApp.owner_name AS owner, v.BusinessApp.namespace AS ns`
	rs, err := sess.Execute(query)
	if err != nil {
		log.Fatalf("query canonical: %v", err)
	}

	for i := 0; i < rs.GetRowSize(); i++ {
		row, _ := rs.GetRowValuesByIndex(i)
		uid, _ := getStr(row, "uid")
		owner, _ := getStr(row, "owner")
		ns, _ := getStr(row, "ns")
		if uid == "" || owner == "" || ns == "" {
			continue
		}

		dupQuery := fmt.Sprintf(`MATCH (v:BusinessApp) WHERE v.BusinessApp.app_name == %s AND v.BusinessApp.namespace == %s AND v.BusinessApp.uid != %s RETURN v.BusinessApp.uid AS uid LIMIT 1`,
			strconv.Quote(owner), strconv.Quote(ns), strconv.Quote(uid))
		dupRs, err := sess.Execute(dupQuery)
		if err != nil || dupRs.GetRowSize() == 0 {
			continue
		}
		dupRow, _ := dupRs.GetRowValuesByIndex(0)
		dupUID, _ := getStr(dupRow, "uid")
		if dupUID == "" {
			continue
		}

		if *dryRun {
			fmt.Printf("would merge: %s -> %s (owner=%s, ns=%s)\n", dupUID, uid, owner, ns)
			continue
		}

		migrateEdge(sess, dupUID, uid, "CallsApp")
		migrateEdge(sess, dupUID, uid, "BelongsToApp")

		delQuery := fmt.Sprintf("DELETE VERTEX %s", strconv.Quote(dupUID))
		if _, err := sess.Execute(delQuery); err != nil {
			log.Printf("delete vertex %s: %v", dupUID, err)
			continue
		}
		fmt.Printf("merged: %s -> %s\n", dupUID, uid)
		merged++
	}

	if *dryRun {
		fmt.Printf("dry-run: %d merges would be performed\n", merged)
	} else {
		fmt.Printf("merged %d duplicate BusinessApp vertices\n", merged)
	}
}

func migrateEdge(sess *nebula.Session, fromUID, toUID, edgeType string) {
	query := fmt.Sprintf("GO FROM %s OVER %s YIELD edge AS e", strconv.Quote(fromUID), edgeType)
	rs, err := sess.Execute(query)
	if err != nil {
		log.Printf("query %s edges from %s: %v", edgeType, fromUID, err)
		return
	}
	for i := 0; i < rs.GetRowSize(); i++ {
		row, _ := rs.GetRowValuesByIndex(i)
		val, _ := row.GetValueByColName("e")
		if val == nil {
			continue
		}
		edgeStr, _ := val.AsString()
		parts := strings.SplitN(edgeStr, "->", 2)
		if len(parts) != 2 {
			continue
		}
		parts[1] = strings.TrimSuffix(parts[1], "@0")
		delQuery := fmt.Sprintf("DELETE EDGE %s %s->%s", edgeType, strconv.Quote(fromUID), strconv.Quote(parts[1]))
		sess.Execute(delQuery)

		insQuery := fmt.Sprintf("INSERT EDGE %s () VALUES %s->%s:()", edgeType, strconv.Quote(toUID), strconv.Quote(parts[1]))
		if _, err := sess.Execute(insQuery); err != nil {
			log.Printf("migrate edge %s %s->%s: %v", edgeType, fromUID, parts[1], err)
		}
	}
	queryIn := fmt.Sprintf("GO FROM %s OVER %s REVERSELY YIELD edge AS e", strconv.Quote(fromUID), edgeType)
	rsIn, err := sess.Execute(queryIn)
	if err != nil {
		return
	}
	for i := 0; i < rsIn.GetRowSize(); i++ {
		row, _ := rsIn.GetRowValuesByIndex(i)
		val, _ := row.GetValueByColName("e")
		if val == nil {
			continue
		}
		edgeStr, _ := val.AsString()
		parts := strings.SplitN(edgeStr, "->", 2)
		if len(parts) != 2 {
			continue
		}
		delQuery := fmt.Sprintf("DELETE EDGE %s %s->%s", edgeType, strconv.Quote(parts[0]), strconv.Quote(fromUID))
		sess.Execute(delQuery)

		insQuery := fmt.Sprintf("INSERT EDGE %s () VALUES %s->%s:()", edgeType, strconv.Quote(parts[0]), strconv.Quote(toUID))
		if _, err := sess.Execute(insQuery); err != nil {
			log.Printf("migrate edge %s %s->%s: %v", edgeType, parts[0], fromUID, err)
		}
	}
}

func parseAddr(addr string) (string, int) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return addr, 9669
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		port = 9669
	}
	return parts[0], port
}

func getStr(row *nebula.Record, col string) (string, error) {
	v, e := row.GetValueByColName(col)
	if e != nil {
		return "", e
	}
	return v.AsString()
}
