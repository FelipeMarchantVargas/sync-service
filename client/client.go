package main

import (
	"context"
	"log"
	"time"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"google.golang.org/grpc"
)

func main() {
	// Conectar al servidor gRPC
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("No se pudo conectar: %v", err)
	}
	defer conn.Close()

	client := pb.NewSyncServiceClient(conn)

	// Llamar a ListFiles
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.ListFiles(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("Error en ListFiles: %v", err)
	}

	log.Println("Archivos en el servidor:", resp.Filenames)
}
