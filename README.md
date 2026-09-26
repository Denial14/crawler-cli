# Crawler CLI
CLI-краулер для обхода веб-сайтов и построения дерева найденных страниц.

## Сборка

```bash
go build -o crawler-cli ./cmd/main.go
```

## Запуск

Linux / macOS:

```bash
./crawler-cli --urls https://go.dev --depth 3 --timeout 1m --request-timeout 5s --output result.json --log crawler.log
```

Windows (PowerShell):

```powershell
.\crawler-cli.exe --urls https://go.dev --depth 3 --timeout 1m --request-timeout 5s --output result.json --log crawler.log
```

Запуск без сборки:

```bash
go run ./cmd/main.go --urls https://example.com --depth 1 --timeout 10s
```

## Флаги

| Флаг | По умолчанию | Описание |
|------|--------------|----------|
| `--urls` | — (обязательный) | Список стартовых URL через запятую |
| `--depth` | `3` | Максимальная глубина рекурсивного обхода |
| `--timeout` | `30s` | Общий таймаут на весь обход |
| `--request-timeout` | `5s` | Таймаут на один HTTP-запрос |
| `--output` | `result.json` | Путь к файлу с результатом |
| `--log` | `crawler.log` | Путь к лог-файлу |

Флаги времени принимают формат: `30s`, `2m`, `1h30m`

Результат сохраняется в JSON-файл, указанный через `--output` (по умолчанию: `result.json`)
Все события записываются в файл, указанный через `--log` (по умолчанию `crawler.log`)

## Остановка

Нажмите `Ctrl+C` во время работы программы. Краулер корректно завершит все воркеры, дособерёт уже полученные результаты и сохранит их в JSON.

## Тесты

```bash
go test ./... -race
```
