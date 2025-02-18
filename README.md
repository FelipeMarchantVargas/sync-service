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
- Protocol Buffers (protoc v3 o superior):
- gRPC para Go: Puedes instalarlo con:

```
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Asegúrate de agregar $GOPATH/bin a tu PATH si no lo has hecho.

# Instalación

Clona el repositorio e instala las dependencias necesarias:

```
git clone https://github.com/FelipeMarchantVargas/sync-service.git
cd sync-service
go mod tidy
```

## Estructura del Proyecto

```
├── client/       # Código fuente del cliente
├── server/       # Código fuente del servidor
|   ├── auth/     # Código para la gestión de autenticación
├── proto/        # Archivos .proto para la definición de los servicios gRPC
├── go.mod        # Archivo de gestión de dependencias de Go
├── go.sum        # Checksum de dependencias
└── README.md     # Documentación del proyecto
```

## Ejecución

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

### Ejemplo de sincronización

Una vez que el servidor esté en ejecución, el cliente puede enviar archivos para sincronización. Un ejemplo básico de uso sería:

```
./client --file /ruta/del/archivo.txt
```

## Mejoras futuras

- Implementación de TLS para mejorar la seguridad.

- Manejo de autenticación con tokens o credenciales.

- Soporte para bases de datos en la sincronización de datos.

- Creación de pruebas unitarias para mejorar la estabilidad del sistema.

## Contribución

Si deseas contribuir, clona el repositorio y realiza un pull request con tus mejoras. ¡Todas las contribuciones son bienvenidas!
