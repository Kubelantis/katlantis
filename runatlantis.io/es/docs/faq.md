# Preguntas frecuentes

**P: ¿Atlantis afecta el [remote state](https://developer.hashicorp.com/terraform/language/state/remote) de Terraform?**

R: No. Atlantis no interfiere con el remote state de Terraform de ninguna manera. Internamente, Atlantis simplemente ejecuta `terraform plan` e `terraform apply`.

**P: ¿Cómo interactúa el locking de Atlantis con el [locking](https://developer.hashicorp.com/terraform/language/state/locking) de Terraform?**

R: Atlantis proporciona locking de pull requests que evita la modificación concurrente de la misma infraestructura (proyecto de Terraform), mientras que el locking de Terraform solo evita que ocurran dos `terraform apply` concurrentes.

El locking de Terraform puede usarse junto con el locking de Atlantis, ya que Atlantis simplemente ejecuta comandos de terraform.

**P: ¿Cómo ejecutar Atlantis en modo de alta disponibilidad? ¿Es necesario?**

R: El servidor Atlantis puede ejecutarse fácilmente bajo la supervisión de un sistema init como `upstart` o `systemd` para asegurar que `atlantis server` siempre esté en ejecución.

Atlantis guarda los locks y el estado de los pull requests en la API de Kubernetes (Leases y recursos `PullStatus`), así que pueden ejecutarse varias réplicas a la vez. Cada pull request lo gestiona una réplica; los webhooks que llegan a otra réplica se reenvían a ella. Consulta [Kubernetes-native Atlantis](https://github.com/App-First-Step/katlantis/blob/master/docs/kubernetes-native.md) y el chart de Helm.

Por defecto, los clones, planes y logs de jobs se guardan en el volumen de cada réplica, y los `apply` se envían a la réplica que hizo el plan. Para que los planes y los logs de jobs terminados estén disponibles en todas las réplicas, y se conserven si se pierde una réplica, activa `--enable-external-stores` con un `plan_store` y un `log_store` compatibles con S3. Si aun así se pierde un plan, ejecuta `atlantis plan` de nuevo: `atlantis apply` no se ejecuta sin un plan que coincida.

**P: ¿Cómo agregar SSL al servidor Atlantis?**

R: Primero, necesitarás obtener un par de claves pública/privada para servir sobre SSL.
Estas necesitan estar en un directorio accesible por Atlantis. Luego inicia `atlantis server` con los flags `--ssl-cert-file` e `--ssl-key-file`.
Consulta `atlantis server --help` para más información.

**P: ¿Atlantis puede detectar drift de infraestructura?**

R: Sí. Cuando se establece el flag `--enable-drift-detection`, Atlantis expone endpoints de API para detección de drift, estado y remediación. La detección de drift funciona ejecutando `terraform plan` contra la branch/ref especificada (fuera del contexto de un pull request) y analizando la salida del plan en busca de cambios. Puedes activar la detección de drift mediante `POST /api/drift/detect` y recuperar resultados en caché mediante `GET /api/drift/status`. Consulta [API Endpoints](api-endpoints.md) para más detalles.

**P: ¿Cómo configuro la detección de drift programada?**

R: Atlantis proporciona la API de detección de drift, pero no incluye un programador integrado. Puedes usar un programador externo (p. ej., cron, pipelines de CI/CD o una herramienta de monitoreo) para llamar periódicamente a `POST /api/drift/detect`. Configura [drift webhooks](sending-notifications-via-webhooks.md) para recibir notificaciones de Slack o HTTP cuando se detecte drift.

**P: ¿Cómo puedo poner Atlantis en marcha en AWS?**

R: Existe el proyecto [terraform-aws-atlantis](https://github.com/terraform-aws-modules/terraform-aws-atlantis) donde se alojan configuraciones completas de Terraform para ejecutar Atlantis en AWS Fargate. Probado y mantenido.
