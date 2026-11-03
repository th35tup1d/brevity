# 🚀 Brevity — Production-Grade High-Throughput URL Shortener

Brevity is a scalable, low-latency URL redirection and analytics engine built from scratch in **Go**, backed by **PostgreSQL** and **Redis**, and containerized with **Docker**. Designed to handle high concurrency with zero hash collisions and non-blocking asynchronous analytics.

---

## 🏗️ Architecture & Technical Highlights

* **Collision-Resilient Core:** Uses sequential auto-incrementing database IDs encoded via **Base62**, completely eliminating collision vulnerabilities common in random hash generators.
* **Cache-Aside Pattern:** Integrates **Redis** to cache hot redirects, ensuring sub-millisecond lookup performance.
* **Asynchronous Click Analytics:** Implements a buffered worker pool pattern in Go to log visitor IP addresses and user agents asynchronously, ensuring **0ms impact** on HTTP redirect latency.
* **API Abuse Prevention:** Protected via a Redis atomic counter rate-limiting middleware.
* **Observability & DevOps:** Fully containerized multi-stage Docker setup with automated health checks and `k6` load-testing validation.

---

## 🛠️ Tech Stack
* **Language:** Go (Golang)
* **Database:** PostgreSQL (`pgx/v5` connection pool)
* **Cache:** Redis (`go-redis/v9`)
* **DevOps:** Docker, Docker Compose, k6

---

## 🚀 Quick Start (Local Development)

1. Clone the repository:
   ```bash
   git clone [https://github.com/yourusername/brevity.git](https://github.com/yourusername/brevity.git)
   cd brevity
