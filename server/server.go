package main

import (
	"log"
	"net"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"google.golang.org/grpc"
)

type SyncServer struct {
	pb.UnimplementedSyncServiceServer
}

func main() {
	// Escuchar en el puerto 50051
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Error al iniciar el servidor: %v", err)
	}

	// Crear el servidor gRPC
	grpcServer := grpc.NewServer()
	pb.RegisterSyncServiceServer(grpcServer, &SyncServer{})

	log.Println("Servidor gRPC corriendo en el puerto 50051")
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Error al iniciar el servidor gRPC: %v", err)
	}
}
