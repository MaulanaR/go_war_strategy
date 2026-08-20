# Frontline Command — Go War Strategy

Prototype RTS parit berbasis web dengan backend Go authoritative dan frontend HTML5 Canvas. Project ini mengambil inspirasi umum dari game lane-based trench warfare, tetapi seluruh kode, UI, nama, dan visual dibuat orisinal.

## Fitur MVP

- Solo melawan AI server-side.
- PvP real-time melalui room dengan kode unik enam karakter.
- Empat tipe unit: Rifleman, Assault, Gunner, dan Tank.
- Ekonomi supply, perlindungan parit, HP markas, kondisi menang/kalah.
- Server authoritative: browser hanya mengirim perintah spawn; simulasi dan validasi terjadi di Go.
- Tampilan responsif tanpa asset eksternal atau framework frontend.
- Health check, graceful shutdown, security headers, Dockerfile, dan test dasar.

## Menjalankan lokal

Persyaratan: Go 1.23 atau lebih baru.

```bash
go mod download
go run .
```

Buka `http://localhost:8080`.

Untuk mencoba PvP di satu komputer, buka dua tab browser. Tab pertama memilih **Buat Room**, lalu tab kedua memasukkan kode room.

## Perintah pengembangan

```bash
make test
make run
make fmt
```

## Arsitektur

```text
Browser (Canvas + JavaScript)
       │ WebSocket: spawn commands / state snapshots
       ▼
Go HTTP + WebSocket server
       │
       ├── Room manager (solo atau PvP)
       ├── Authoritative game loop, 20 tick/detik
       ├── AI commander untuk mode solo
       └── Broadcast snapshot, 10 kali/detik
```

Struktur utama:

- `internal/game`: aturan simulasi, unit, resource, parit, damage, dan kemenangan.
- `internal/server`: room manager, koneksi WebSocket, AI, serta HTTP handler.
- `web`: lobby, HUD, renderer Canvas, dan kontrol pemain.
- `main.go`: embed asset frontend dan menjalankan server.

## Protokol WebSocket ringkas

Koneksi:

- Solo: `/ws?mode=solo`
- Membuat room: `/ws?mode=create`
- Bergabung: `/ws?mode=join&code=ABC123`

Perintah client:

```json
{"type":"spawn","unit":"rifleman"}
```

Server mengirim `welcome`, `notice`, `error`, dan snapshot `state`.

## Deployment dengan Docker

```bash
docker build -t go-war-strategy .
docker run --rm -p 8080:8080 go-war-strategy
```

Server membaca environment variable `PORT` dan default ke `8080`.

## Roadmap yang disarankan

1. Sistem akun, reconnect token, dan penyimpanan hasil pertandingan.
2. Matchmaking publik dan spectator mode.
3. Peta/data unit berbasis JSON agar balancing tidak memerlukan compile ulang.
4. Artillery ability, fog of war, officer aura, dan variasi objective.
5. Replay deterministik, anti-cheat telemetry, rate limit, serta Redis room registry untuk multi-instance.
6. Asset pixel-art orisinal, audio, tutorial, campaign, dan editor sandbox.

## Catatan legal dan desain

Jangan menyalin sprite, audio, level, teks, merek, atau identitas visual dari game komersial yang menjadi referensi. Mekanik genre dapat dijadikan inspirasi, tetapi produk akhir sebaiknya memiliki tema, balancing, art direction, serta konten yang jelas berbeda.
