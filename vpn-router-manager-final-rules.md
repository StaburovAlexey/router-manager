# Итоговые правила для разработки VPN Router Manager

## 1. Назначение проекта

Нужно создать приложение **VPN Router Manager** на Go.

Приложение должно работать на Linux-мини-ПК, который используется как Wi-Fi VPN-роутер.

Мини-ПК должен:

- принимать интернет по Ethernet;
- раздавать интернет по Wi-Fi;
- пропускать интернет мини-ПК и Wi-Fi клиентов через VPN-цепочку;
- уметь отключать VPN и пускать интернет напрямую;
- управлять RU-сервером и foreign-серверами по SSH;
- иметь TUI-интерфейс на русском языке;
- иметь CLI-команды для быстрого управления;
- показывать итоговую информацию после настройки;
- сохранять эту информацию и показывать её повторно через `sudo vpn-router info`.

---

## 2. Общая схема

```text
Wi-Fi клиенты
    ↓
Мини-ПК роутер
    ↓ Ethernet
RU-сервер
    ↓
foreign-сервер 1 / foreign-сервер 2 / foreign-сервер 3
    ↓
Интернет
```

Мини-ПК является главным узлом управления.

Go-приложение ставится **только на мини-ПК**.

RU и foreign-серверы управляются удалённо по SSH.

---

## 3. Главный принцип установки

Bash-установщика быть не должно.

Никакого обязательного файла:

```text
install.sh
```

не требуется.

Приложение распространяется как один Go-бинарник:

```text
vpn-router
```

Первичный сценарий:

```bash
chmod +x vpn-router
sudo ./vpn-router bootstrap
sudo vpn-router setup
sudo vpn-router
```

Где:

```text
bootstrap  → подготовка системы и установка самого приложения
setup      → пошаговая настройка мини-ПК, RU и foreign-серверов
vpn-router → запуск TUI-интерфейса
```

---

## 4. Команда bootstrap

Команда:

```bash
sudo ./vpn-router bootstrap
```

или после установки:

```bash
sudo vpn-router bootstrap
```

должна:

1. проверить root-права;
2. проверить ОС;
3. проверить наличие `apt`;
4. установить системные зависимости;
5. установить или проверить `sing-box`;
6. создать `/etc/vpn-router`;
7. выставить права;
8. скопировать бинарник в `/usr/local/sbin/vpn-router`;
9. проверить доступность команды `vpn-router`;
10. предложить запустить:

```bash
sudo vpn-router setup
```

---

## 5. Поддерживаемые ОС

Целевая поддержка:

```text
Ubuntu 22.04 / 24.04
Debian 12
```

Если ОС не поддерживается, приложение должно вывести понятную ошибку на русском языке.

---

## 6. Системные зависимости

`bootstrap` должен установить:

```bash
apt update
apt install -y \
  iproute2 \
  nftables \
  hostapd \
  dnsmasq \
  curl \
  openssh-client \
  qrencode \
  ca-certificates \
  iputils-ping \
  dnsutils \
  openssl
```

`jq` не обязателен, потому что JSON должен обрабатываться самим Go-приложением.

---

## 7. Установка sing-box

Go-приложение должно уметь установить `sing-box` без Bash-скрипта.

Рекомендуемая логика:

```text
1. Определить архитектуру: amd64/arm64
2. Скачать выбранную версию sing-box release
3. Распаковать бинарник
4. Положить его в /usr/local/bin/sing-box
5. Создать systemd unit, если его нет
6. Проверить sing-box version
```

Путь:

```text
/usr/local/bin/sing-box
```

---

## 8. Команда setup

Команда:

```bash
sudo vpn-router setup
```

должна пошагово настроить:

```text
- мини-ПК как Wi-Fi роутер;
- RU-сервер как relay;
- foreign-серверы как exit nodes;
- VPN-режим через rule_set;
- direct-режим;
- custom direct rules;
- VLESS/REALITY ссылку;
- QR-код;
- финальный install-summary.
```

---

## 9. Язык интерфейса

### TUI

TUI-интерфейс должен быть полностью на русском языке:

```text
- меню;
- подсказки;
- ошибки;
- подтверждения;
- описания действий;
- help-тексты;
- финальный отчёт.
```

### CLI

CLI-команды должны быть на английском языке:

```bash
sudo vpn-router vpn
sudo vpn-router direct
sudo vpn-router status
```

Но описание команд в TUI и `info` должно быть на русском языке.

---

## 10. Запуск TUI

Если команда запущена без аргументов:

```bash
sudo vpn-router
```

должен открываться TUI-интерфейс.

Главное меню:

```text
VPN Router Manager

1) Статус системы
2) Включить VPN-режим
3) Отключить VPN / включить прямой интернет
4) Логи
5) Информация, VLESS-ссылка и QR-код

6) Правила сайтов без VPN
7) Управление RU-сервером
8) Foreign-серверы
9) Настройки Wi-Fi роутера
10) Резервные копии и откат
11) Диагностика

0) Выход
```

---

## 11. Основные CLI-команды

```bash
sudo vpn-router
sudo vpn-router bootstrap
sudo vpn-router setup

sudo vpn-router vpn
sudo vpn-router direct
sudo vpn-router status
sudo vpn-router logs
sudo vpn-router info
sudo vpn-router qr
```

---

## 12. VPN-режим

Команда:

```bash
sudo vpn-router vpn
```

должна включать VPN-режим.

Важно:

```text
sudo vpn-router vpn всегда работает через rule_set
```

Отдельной команды `geo` быть не должно.

Логика VPN-режима:

```text
служебные адреса VPN-инфраструктуры → direct
локальная сеть                      → direct
custom direct правила               → direct
всё остальное                       → VPN → RU → foreign
```

Обязательные direct-исключения:

```text
RU_SERVER_IP
LAN subnet
private IP ranges
DHCP/DNS local traffic
```

Это нужно, чтобы избежать routing loop.

---

## 13. Direct-режим

Команда:

```bash
sudo vpn-router direct
```

должна отключать VPN и пускать интернет напрямую:

```text
мини-ПК + Wi-Fi клиенты → Ethernet → интернет без VPN
```

При включении direct-режима приложение должно:

1. остановить VPN-сервис или применить direct-конфиг;
2. применить direct nftables rules;
3. вернуть обычный default route;
4. вернуть обычный DNS;
5. сохранить `current_mode = direct`.

---

## 14. Отдельной команды geo нет

Команды:

```bash
sudo vpn-router geo
```

быть не должно.

GEO/rule-set поведение является частью:

```bash
sudo vpn-router vpn
```

---

## 15. Пользовательские сайты/IP без VPN

Пользователь должен иметь возможность добавлять сайты и IP, которые будут работать без VPN даже в VPN-режиме.

Команды:

```bash
sudo vpn-router direct-add domain <domain>
sudo vpn-router direct-add suffix <domain>
sudo vpn-router direct-add ip <ip>
sudo vpn-router direct-add cidr <cidr>
sudo vpn-router direct-remove <value>
sudo vpn-router direct-list
sudo vpn-router direct-edit
```

Примеры:

```bash
sudo vpn-router direct-add suffix gosuslugi.ru
sudo vpn-router direct-add suffix sberbank.ru
sudo vpn-router direct-add domain login.example.com
sudo vpn-router direct-add ip 1.2.3.4
sudo vpn-router direct-add cidr 203.0.113.0/24
```

Файл правил:

```text
/etc/vpn-router/rules/custom-direct.json
```

Пример:

```json
{
  "version": 3,
  "rules": [
    {
      "domain_suffix": [
        "gosuslugi.ru",
        "sberbank.ru"
      ]
    },
    {
      "domain": [
        "login.example.com"
      ]
    },
    {
      "ip_cidr": [
        "1.2.3.4/32",
        "203.0.113.0/24"
      ]
    }
  ]
}
```

После изменения правил приложение должно:

1. проверить JSON;
2. проверить sing-box config;
3. сделать backup;
4. если активен VPN-режим — перезапустить sing-box;
5. показать результат.

Ручной команды `rules-update` быть не должно.

---

## 16. RU-сервер

RU-сервер управляется только с мини-ПК.

Команды:

```bash
sudo vpn-router ru auto
sudo vpn-router ru use <foreign-name>
sudo vpn-router ru list
sudo vpn-router ru test <foreign-name>
sudo vpn-router ru status
sudo vpn-router ru logs
sudo vpn-router ru rollback
```

Примеры:

```bash
sudo vpn-router ru auto
sudo vpn-router ru use de-1
sudo vpn-router ru use nl-1
sudo vpn-router ru test de-1
sudo vpn-router ru status
sudo vpn-router ru logs
```

RU-сервер должен уметь:

```text
- принимать трафик от мини-ПК;
- отправлять трафик на foreign-серверы;
- работать в auto-режиме;
- работать в manual-режиме;
- безопасно переключаться между foreign-серверами;
- откатывать конфиг при ошибке.
```

На RU-сервере должны быть файлы:

```text
/etc/ru-vpn/
├─ config.json
├─ foreign-servers.json
├─ state.json
└─ backups/
```

---

## 17. Foreign-серверы

Управление foreign-серверами выполняется с мини-ПК.

Команды:

```bash
sudo vpn-router foreign add
sudo vpn-router foreign remove <foreign-name>
sudo vpn-router foreign remove <foreign-name> --switch-auto
sudo vpn-router foreign list
sudo vpn-router foreign test <foreign-name>
sudo vpn-router foreign cleanup <foreign-name>
```

### Добавление foreign-сервера

Команда:

```bash
sudo vpn-router foreign add
```

должна спросить:

```text
- имя сервера, например de-1;
- IP;
- SSH user;
- SSH port;
- VPN port, по умолчанию 443.
```

SNI пользователь не вводит.

### Удаление foreign-сервера

Команда:

```bash
sudo vpn-router foreign remove de-1
```

должна удалить сервер из схемы.

Если сервер сейчас выбран вручную на RU, приложение должно остановить удаление и предложить:

```bash
sudo vpn-router foreign remove de-1 --switch-auto
```

### Очистка VPS

Команда:

```bash
sudo vpn-router foreign cleanup de-1
```

должна спросить подтверждение:

```text
Введите YES для продолжения:
```

Без `YES` ничего не удалять.

---

## 18. Автоматический SNI / mask domain

SNI не должен вводиться руками.

Приложение должно иметь список кандидатов:

```text
www.microsoft.com
www.apple.com
www.cloudflare.com
www.amazon.com
www.bing.com
www.office.com
www.mozilla.org
```

Алгоритм:

```text
1. Перебрать список кандидатов
2. Проверить DNS
3. Проверить порт 443
4. Проверить TLS handshake
5. Выбрать первый рабочий
6. Сохранить в config
7. Показать в info
```

Пример:

```text
SNI: auto
Выбран: www.microsoft.com
```

---

## 19. SSH-ключи

Во время `setup` приложение должно объяснить пользователю, что для управления RU и foreign-серверами нужен SSH-доступ по ключам.

Показать команды:

```bash
ssh-keygen -t ed25519 -C "vpn-router"
ssh-copy-id -p 22 root@RU_SERVER_IP
ssh-copy-id -p 22 root@FOREIGN_SERVER_IP
```

Потом ждать ввод:

```text
done
```

После этого проверить доступ:

```bash
ssh -o BatchMode=yes -o ConnectTimeout=8 -p <port> <user>@<ip> "echo ok"
```

Если SSH-доступ по ключу не работает — setup не продолжается.

---

## 20. Wi-Fi роутер

Приложение должно:

```text
1. Проверить наличие Ethernet
2. Проверить наличие интернета по Ethernet
3. Найти Wi-Fi модули
4. Проверить поддержку AP mode
5. Если модулей несколько — дать выбрать AP-модуль
6. Выбранный Wi-Fi модуль использовать только для раздачи
7. Остальные Wi-Fi модули не трогать
8. Настроить hostapd
9. Настроить dnsmasq
10. Настроить nftables
```

Параметры, которые спрашивает setup:

```text
SSID Wi-Fi сети
Пароль Wi-Fi
LAN subnet, например 10.77.0.0/24
LAN gateway, например 10.77.0.1
```

---

## 21. Настройка диапазона, канала и скорости Wi-Fi

Приложение должно позволять менять параметры Wi-Fi точки доступа, чтобы пользователь мог подобрать лучшую скорость.

Нужно поддержать:

```text
- диапазон: 2.4 GHz / 5 GHz, если адаптер поддерживает;
- канал Wi-Fi;
- ширину канала: 20 / 40 / 80 MHz, если поддерживается;
- страну regulatory domain, например RU / DE;
- режим Wi-Fi: n / ac / ax, если поддерживается адаптером.
```

---

## 22. CLI-команды Wi-Fi

Добавить команды:

```bash
sudo vpn-router wifi status
sudo vpn-router wifi scan
sudo vpn-router wifi set-band 2.4
sudo vpn-router wifi set-band 5
sudo vpn-router wifi set-channel <channel>
sudo vpn-router wifi set-width <20|40|80>
sudo vpn-router wifi auto-channel
sudo vpn-router wifi restart
```

Примеры:

```bash
sudo vpn-router wifi scan
sudo vpn-router wifi auto-channel
sudo vpn-router wifi set-band 5
sudo vpn-router wifi set-channel 36
sudo vpn-router wifi set-width 80
sudo vpn-router wifi restart
```

---

## 23. TUI-раздел Wi-Fi

Раздел TUI:

```text
Настройки Wi-Fi роутера

1) Показать настройки Wi-Fi
2) Изменить SSID
3) Изменить пароль Wi-Fi
4) Изменить диапазон Wi-Fi
5) Изменить канал Wi-Fi
6) Изменить ширину канала
7) Автоподбор лучшего канала
8) Сканировать соседние Wi-Fi сети
9) Перезапустить Wi-Fi точку доступа
10) Проверить Wi-Fi модуль
11) Показать подключённых клиентов

0) Назад
```

---

## 24. Автоподбор Wi-Fi канала

Команда:

```bash
sudo vpn-router wifi auto-channel
```

должна:

```text
1. Просканировать соседние Wi-Fi сети
2. Посчитать загруженность каналов
3. Проверить поддерживаемые каналы адаптера
4. Исключить неподдерживаемые и запрещённые каналы
5. Предложить лучший канал
6. Предложить диапазон и ширину канала
7. Спросить подтверждение
8. Перегенерировать hostapd.conf
9. Перезапустить hostapd
10. Показать результат
```

Пример вывода:

```text
Сканирование Wi-Fi окружения...

Найдено сетей:
  2.4 GHz: 18
  5 GHz: 6

Рекомендация:
  Диапазон: 5 GHz
  Канал: 36
  Ширина: 80 MHz

Применить настройки? [y/N]
```

---

## 25. Предупреждение при изменении Wi-Fi

При изменении канала, диапазона, ширины канала, SSID или пароля приложение должно предупреждать:

```text
Внимание: изменение настроек перезапустит Wi-Fi точку доступа.
Подключённые устройства временно потеряют соединение.
Продолжить?
```

---

## 26. Проверка возможностей Wi-Fi адаптера

Перед показом опций приложение должно проверить адаптер через:

```bash
iw list
```

Нужно определить:

```text
- поддерживает ли AP mode;
- поддерживает ли 5 GHz;
- какие каналы доступны;
- какие ширины канала доступны;
- поддерживает ли HT/VHT/HE;
- можно ли использовать 802.11n/ac/ax.
```

Если адаптер не поддерживает 5 GHz, TUI не должен предлагать 5 GHz.

---

## 27. Рекомендации по Wi-Fi каналам

Для 2.4 GHz по умолчанию использовать только:

```text
1
6
11
```

Для 5 GHz по умолчанию предпочитать:

```text
36
40
44
48
```

DFS-каналы не выбирать по умолчанию, потому что точка доступа может ждать проверку радара или внезапно сменить канал.

---

## 28. Wi-Fi config.json

В `/etc/vpn-router/config.json` должен быть блок:

```json
{
  "wifi": {
    "country": "RU",
    "band": "5",
    "channel": 36,
    "channel_width": 80,
    "hw_mode": "a",
    "ieee80211n": true,
    "ieee80211ac": true
  }
}
```

---

## 29. Примеры hostapd.conf

### 2.4 GHz

```conf
country_code=RU
interface=wlan0
driver=nl80211
ssid=MyVPNRouter
hw_mode=g
channel=6
ieee80211n=1
wmm_enabled=1
```

### 5 GHz

```conf
country_code=RU
interface=wlan0
driver=nl80211
ssid=MyVPNRouter
hw_mode=a
channel=36
ieee80211n=1
ieee80211ac=1
vht_oper_chwidth=1
wmm_enabled=1
```

---

## 30. Kill switch

В VPN-режиме трафик, который должен идти через VPN, не должен уходить напрямую через Ethernet при падении туннеля.

Логика:

```text
VPN работает:
  Wi-Fi клиенты → rule_set → VPN/direct по правилам

VPN упал:
  VPN-трафик не должен утекать напрямую
```

В direct-режиме:

```text
мини-ПК + Wi-Fi клиенты → Ethernet напрямую
```

---

## 31. Финальный отчёт после setup

После успешного `setup` приложение должно:

1. вывести полный отчёт в терминал;
2. сохранить его в:

```text
/etc/vpn-router/install-summary.txt
```

3. показать команду:

```bash
sudo vpn-router info
```

---

## 32. Что должно быть в `sudo vpn-router info`

Команда:

```bash
sudo vpn-router info
```

должна показывать:

```text
- что настроено на мини-ПК;
- WAN interface;
- AP interface;
- Wi-Fi SSID;
- Wi-Fi band;
- Wi-Fi channel;
- Wi-Fi channel width;
- LAN subnet;
- LAN gateway;
- RU-сервер;
- foreign-серверы;
- выбранные SNI;
- текущий режим;
- VPN policy;
- VLESS/REALITY ссылка;
- команда QR;
- все CLI-команды;
- команды Wi-Fi;
- примеры direct-add;
- где лежат конфиги;
- как снова открыть эту информацию.
```

---

## 33. VLESS/REALITY ссылка и QR

После setup приложение должно создать VLESS/REALITY ссылку:

```text
vless://...
```

Ссылка нужна для прямого подключения к RU-серверу через клиенты:

```text
v2rayN
v2rayNG
NekoBox
Hiddify
FoXray
```

Wi-Fi клиентам ссылка не нужна, если они используют мини-ПК как роутер.

Команда QR:

```bash
sudo vpn-router qr
```

должна вывести QR-код в терминал.

---

## 34. Конфиги на мини-ПК

```text
/etc/vpn-router/
├─ config.json
├─ foreign-servers.json
├─ install-summary.txt
├─ client-link.txt
├─ rules/
│  ├─ vpn-infra.json
│  ├─ custom-direct.json
│  └─ custom-proxy.json
├─ modes/
│  ├─ vpn.json
│  └─ direct.json
└─ backups/
```

Права:

```bash
chmod 700 /etc/vpn-router
chmod 600 /etc/vpn-router/config.json
chmod 600 /etc/vpn-router/install-summary.txt
chmod 600 /etc/vpn-router/client-link.txt
```

---

## 35. Пример config.json

```json
{
  "mini_pc": {
    "wan_interface": "enp1s0",
    "ap_interface": "wlan0",
    "ssid": "MyVPNRouter",
    "lan_cidr": "10.77.0.0/24",
    "lan_gateway": "10.77.0.1"
  },
  "wifi": {
    "country": "RU",
    "band": "5",
    "channel": 36,
    "channel_width": 80,
    "hw_mode": "a",
    "ieee80211n": true,
    "ieee80211ac": true
  },
  "ru_server": {
    "ip": "45.xx.xx.xx",
    "ssh_user": "root",
    "ssh_port": 22,
    "vpn_port": 443
  },
  "vpn_policy": {
    "mode": "rule_set",
    "custom_direct_enabled": true
  },
  "current_mode": "direct"
}
```

---

## 36. Пример foreign-servers.json

```json
{
  "servers": [
    {
      "name": "de-1",
      "ip": "185.xx.xx.xx",
      "ssh_user": "root",
      "ssh_port": 22,
      "vpn_port": 443,
      "reality": {
        "sni": "www.microsoft.com",
        "public_key": "PUBLIC_KEY",
        "short_id": "SHORT_ID"
      }
    }
  ]
}
```

---

## 37. Безопасное применение конфигов

Любое изменение должно идти через безопасный сценарий:

```text
1. Сгенерировать временный конфиг
2. Проверить конфиг через sing-box check
3. Сделать backup текущего конфига
4. Применить новый конфиг
5. Перезапустить systemd service
6. Проверить active status
7. Если ошибка — rollback
```

Это обязательно для:

```text
- bootstrap;
- setup;
- vpn/direct;
- direct rules;
- ru auto;
- ru use;
- foreign add;
- foreign remove;
- foreign cleanup;
- wifi set-band;
- wifi set-channel;
- wifi set-width;
- wifi auto-channel.
```

---

## 38. Диагностика

Команда:

```bash
sudo vpn-router status
```

должна показывать:

```text
- текущий режим: vpn/direct;
- WAN interface;
- AP interface;
- Wi-Fi SSID;
- Wi-Fi band/channel/width;
- sing-box status;
- hostapd status;
- dnsmasq status;
- nftables status;
- RU server;
- active foreign mode: auto/manual;
- selected foreign;
- current public IP;
- DNS status;
- количество Wi-Fi клиентов, если возможно.
```

Команда:

```bash
sudo vpn-router logs
```

показывает локальные логи.

Команда:

```bash
sudo vpn-router ru logs
```

показывает логи RU-сервера через SSH.

---

## 39. Структура Go-проекта

```text
vpn-router-manager/
├─ go.mod
├─ go.sum
├─ cmd/
│  └─ vpn-router/
│     └─ main.go
├─ internal/
│  ├─ app/
│  ├─ bootstrap/
│  ├─ tui/
│  ├─ cli/
│  ├─ config/
│  ├─ shell/
│  ├─ sshclient/
│  ├─ network/
│  ├─ wifi/
│  ├─ singbox/
│  ├─ nftables/
│  ├─ rules/
│  ├─ ru/
│  ├─ foreign/
│  ├─ reality/
│  ├─ summary/
│  └─ diagnostics/
└─ templates/
   ├─ singbox-local.json.tmpl
   ├─ singbox-ru.json.tmpl
   ├─ singbox-foreign.json.tmpl
   ├─ hostapd.conf.tmpl
   ├─ dnsmasq.conf.tmpl
   └─ nftables.nft.tmpl
```

`install.sh` в проекте не нужен.

---

## 40. Сборка

Для Linux amd64:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/vpn-router-linux-amd64 ./cmd/vpn-router
```

Для Linux arm64:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o dist/vpn-router-linux-arm64 ./cmd/vpn-router
```

---

## 41. Пользовательский сценарий

### 1. Получить бинарник

```bash
scp vpn-router-linux-amd64 user@MINI_PC_IP:/tmp/vpn-router
```

### 2. На мини-ПК

```bash
cd /tmp
chmod +x vpn-router
sudo ./vpn-router bootstrap
```

### 3. После bootstrap

```bash
sudo vpn-router setup
```

### 4. Открыть TUI

```bash
sudo vpn-router
```

### 5. Управление

```bash
sudo vpn-router vpn
sudo vpn-router direct
sudo vpn-router status
sudo vpn-router info
```

---

## 42. Критерии готовности

Проект считается готовым, если:

```text
- нет Bash-установщика;
- приложение распространяется как один Go-бинарник;
- sudo ./vpn-router bootstrap устанавливает системные зависимости и само приложение;
- sudo vpn-router setup настраивает мини-ПК, RU и foreign-серверы;
- sudo vpn-router открывает русскоязычный TUI;
- Wi-Fi роутер работает;
- можно менять Wi-Fi диапазон, канал и ширину канала;
- есть автоподбор лучшего Wi-Fi канала;
- VPN-режим работает через rule_set;
- direct-режим отключает VPN и пускает интернет напрямую;
- custom direct правила работают;
- RU auto/manual переключается с мини-ПК;
- foreign add/remove работает с мини-ПК;
- SNI выбирается автоматически;
- SSH-ключи объясняются и проверяются;
- финальный отчёт сохраняется и показывается через sudo vpn-router info;
- QR показывается через sudo vpn-router qr;
- при ошибке есть backup/rollback;
- чувствительные файлы имеют права 600;
- /etc/vpn-router имеет права 700.
```

---

## 43. Важные ограничения

```text
- Не использовать install.sh.
- Не делать всё Bash-скриптом.
- Основная логика только в Go.
- Go-приложение устанавливается только на мини-ПК.
- RU и foreign управляются по SSH.
- TUI полностью на русском.
- CLI-команды на английском.
- Отдельной команды geo нет.
- sudo vpn-router vpn всегда использует rule_set.
- Ручной команды rules-update нет.
- SNI руками не спрашивать.
- Все команды показывать после setup и в sudo vpn-router info.
- Wi-Fi канал, диапазон и ширина должны меняться через TUI и CLI.
```

---

## 44. Короткое резюме

Нужно создать Go-приложение `vpn-router`.

Оно должно быть одним бинарником и работать без Bash-установщика.

Первичная установка:

```bash
chmod +x vpn-router
sudo ./vpn-router bootstrap
sudo vpn-router setup
sudo vpn-router
```

Приложение должно:

```text
- превращать мини-ПК в Wi-Fi VPN-роутер;
- управлять RU и foreign-серверами по SSH;
- иметь русский TUI;
- иметь CLI-команды;
- использовать sing-box, VLESS/REALITY и rule_set;
- поддерживать VPN/direct режимы;
- поддерживать custom direct сайты/IP;
- добавлять/удалять foreign-серверы;
- переключать RU auto/manual;
- автоматически выбирать SNI;
- позволять менять Wi-Fi диапазон, канал и ширину канала;
- уметь подбирать лучший Wi-Fi канал;
- показывать финальный отчёт, VLESS-ссылку и QR;
- безопасно применять конфиги через check/backup/rollback.
```
