# Router Manager

Go-приложение для настройки мини-ПК как Wi-Fi роутера.

## Первый запуск на устройстве

```bash
chmod +x router-manager
sudo ./router-manager
```

При первом запуске приложение подготовит систему, покажет чеклист готовности и откроет пошаговую настройку. Если настройка оборвётся, снова запустите `sudo router-manager`: мастер продолжит с уже сохранёнными значениями.

Перед запуском подготовьте:

- мини-ПК с Ubuntu 22.04/24.04 или Debian 12;
- интернет на мини-ПК;
- Wi-Fi адаптер для раздачи сети;
- IP, SSH-пользователя и пароль или ключи для входного и выходного VPS.

## Обычное использование

```bash
sudo router-manager
```

Открыть меню. Основные разделы:

- `Интернет` - включить маршрут через сервер или прямой интернет, проверить состояние;
- `Подключить устройство` - посмотреть Wi-Fi сеть и QR-код клиента;
- `Сайты прямого доступа` - добавить сайт или IP, который должен идти напрямую;
- `Wi-Fi` - посмотреть настройки, сменить адаптер раздачи, подобрать канал, перезапустить точку доступа;
- `Проблемы и диагностика` - собрать отчёт без секретов, открыть логи, откатить локальную сеть.

## Основные команды

```bash
sudo router-manager
```

Открыть TUI-меню. Если приложение ещё не настроено, начнётся первичная настройка.

```bash
sudo router-manager tunnel
```

Включить режим маршрутизации.

```bash
sudo router-manager direct
```

Отключить маршрут через сервер и включить прямой интернет.

```bash
sudo router-manager status
```

Показать статус системы, Wi-Fi, маршрутизации, DNS и сервисов.

```bash
sudo router-manager report
```

Собрать отчёт диагностики без секретов.

```bash
sudo router-manager logs
```

Показать локальные логи `sing-box`, `hostapd` и `dnsmasq`.

```bash
sudo router-manager info
```

Показать итоговую информацию по текущей настройке.

```bash
sudo router-manager qr
```

Показать QR-код для подключения клиента.

```bash
sudo router-manager update
```

Скачать и установить последний GitHub Release.

```bash
sudo router-manager restore-network
```

Откатить локальные сетевые изменения router-manager.

```bash
sudo router-manager uninstall
```

Полностью удалить router-manager с устройства: откатить локальную сеть, удалить настройки, данные, бинарник, локальный sing-box и прикладные зависимости. Системно важные пакеты вроде `iproute2`, `openssh-client`, `openssl`, `curl` не удаляются. Удалённые серверы не изменяются.

## Правила сайтов прямого доступа

```bash
sudo router-manager direct-add site gosuslugi.ru
sudo router-manager direct-add site https://login.example.com/path
sudo router-manager direct-add site 1.2.3.4
sudo router-manager direct-add site 203.0.113.0/24
```

Добавить сайт, IP или подсеть прямого доступа. Приложение само распознаёт тип значения.

Расширенные варианты:

```bash
sudo router-manager direct-add suffix example.com
sudo router-manager direct-add domain login.example.com
sudo router-manager direct-add ip 1.2.3.4
sudo router-manager direct-add cidr 203.0.113.0/24
```

```bash
sudo router-manager direct-remove <value>
```

Удалить правило прямого доступа.

```bash
sudo router-manager direct-list
```

Показать текущие правила прямого доступа.

```bash
sudo router-manager direct-list --json
```

Показать правила в исходном JSON-формате.

```bash
sudo router-manager direct-edit
```

Открыть `custom-direct.json` в редакторе.

## Входной сервер

```bash
sudo router-manager ru auto
```

Включить автоматический выбор выходного сервера.

```bash
sudo router-manager ru use <server-name>
```

Выбрать выходной сервер вручную.

```bash
sudo router-manager ru list
```

Показать выходные серверы.

```bash
sudo router-manager ru test <server-name>
```

Проверить выходной сервер.

```bash
sudo router-manager ru status
```

Показать статус входного сервера.

```bash
sudo router-manager ru logs
```

Показать логи входного сервера.

```bash
sudo router-manager ru rollback
```

Откатить конфиг входного сервера из backup.

## Выходные серверы

```bash
sudo router-manager foreign add
```

Добавить выходной сервер.

```bash
sudo router-manager foreign remove <server-name>
sudo router-manager foreign remove <server-name> --switch-auto
```

Удалить выходной сервер из схемы. `--switch-auto` переключает входной сервер в auto перед удалением.

```bash
sudo router-manager foreign list
```

Показать выходные серверы.

```bash
sudo router-manager foreign test <server-name>
```

Проверить SSH-доступ к выходному серверу.

```bash
sudo router-manager foreign cleanup <server-name>
```

Очистить VPS выходного сервера: остановить `sing-box` и перенести активный config в backup.

## Wi-Fi

```bash
sudo router-manager wifi status
```

Показать настройки Wi-Fi.

```bash
sudo router-manager wifi scan
```

Сканировать соседние Wi-Fi сети.

```bash
sudo router-manager wifi adapters
```

Показать Wi-Fi адаптеры, их возможности и рекомендованные настройки.

```bash
sudo router-manager wifi set-adapter <interface>
```

Сменить Wi-Fi адаптер для раздачи. Приложение проверит поддержку AP mode, подберёт диапазон, канал и ширину, применит настройки и вернёт старый адаптер в NetworkManager. Если новая точка доступа не запустится, настройки будут возвращены на старый адаптер.

```bash
sudo router-manager wifi set-band 2.4
sudo router-manager wifi set-band 5
```

Изменить диапазон Wi-Fi.

```bash
sudo router-manager wifi set-channel <channel>
```

Изменить канал Wi-Fi.

```bash
sudo router-manager wifi set-width <20|40|80>
```

Изменить ширину канала.

```bash
sudo router-manager wifi auto-channel
```

Подобрать лучший канал автоматически.

```bash
sudo router-manager wifi restart
```

Перезапустить Wi-Fi точку доступа.

## Сборка

```bash
make test
make build
make build-all
```

- `make test` - запускает все Go-тесты.
- `make build` - собирает Linux amd64 бинарник в `dist/router-manager-linux-amd64`.
- `make build-all` - собирает Linux amd64 и arm64 бинарники.
- `make deploy` - собирает amd64 и копирует бинарник на устройство из переменной `TARGET`.

Сборка с версией для релиза:

```bash
make build-all VERSION=v0.1.1
```

## Релиз

Релиз создаётся тегом:

```bash
git checkout main
git merge dev
git push origin main
git tag v0.1.1
git push origin v0.1.1
```

GitHub Actions соберёт бинарники и создаст GitHub Release.

Для следующей версии:

```bash
git tag v0.1.2
git push origin v0.1.2
```
