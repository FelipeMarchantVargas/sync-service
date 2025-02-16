# Sync Service

Sync Service es un servicio basado en gRPC escrito en Go, diseñado para sincronizar datos entre un cliente y un servidor de manera eficiente.

## Características

- Comunicación basada en gRPC.
- Soporte para sincronización de datos entre cliente y servidor.
- Código modular y escalable.
- Diseñado para eficiencia y seguridad.

## Requisitos

Antes de ejecutar el proyecto, asegúrate de tener instalado:

- Go (>=1.18)
- gRPC y Protobuf

Para instalar las dependencias necesarias, ejecuta:

```
go mod tidy
```

## Estructura del Proyecto

```
├── client/       # Código fuente del cliente
├── server/       # Código fuente del servidor
├── proto/        # Archivos .proto para la definición de los servicios gRPC
├── go.mod        # Archivo de gestión de dependencias de Go
├── go.sum        # Checksum de dependencias
└── README.md     # Documentación del proyecto
```

## Instalación y Ejecución

### 1. Iniciar el Servidor

Ejecuta el siguiente comando para iniciar el servidor gRPC:

```
go run server/server.go
```

### 2. Ejecutar el Cliente

Para iniciar el cliente y enviar datos al servidor:

```
go run client/client.go
```

## Uso

El cliente se conecta al servidor y envía datos de sincronización, recibiendo una confirmación por parte del servidor.

Ejemplo de solicitud en client/client.go:

```
response, err := client.SyncData(ctx, &pb.SyncRequest{Data: "Ejemplo de sincronización"})
if err != nil {
    log.Fatalf("Error en la sincronización: %v", err)
}
log.Printf("Respuesta del servidor: %v", response.Status)
```

## Mejoras futuras

- Implementación de TLS para mejorar la seguridad.

- Manejo de autenticación con tokens o credenciales.

- Soporte para bases de datos en la sincronización de datos.

- Creación de pruebas unitarias para mejorar la estabilidad del sistema.

## Contribución

Si deseas contribuir, clona el repositorio y realiza un pull request con tus mejoras. ¡Todas las contribuciones son bienvenidas!
