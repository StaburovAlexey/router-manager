# Router Manager

Router Manager настраивает мини-ПК как Wi-Fi роутер и управляет маршрутизацией через выбранные серверы.

Приложение рассчитано на Ubuntu 22.04/24.04 или Debian 12. Для установки нужен пользователь с `sudo`, интернет на мини-ПК и Wi-Fi адаптер, который умеет работать в режиме точки доступа.

## Быстрая Установка

Откройте страницу релизов:

https://github.com/StaburovAlexey/router-manager/releases/latest

Скачайте файл под архитектуру устройства:

- `router-manager-linux-amd64` - обычные Intel/AMD мини-ПК.
- `router-manager-linux-arm64` - ARM-устройства.

Можно скачать прямо из терминала:

```bash
ARCH="$(dpkg --print-architecture)"
curl -L -o router-manager "https://github.com/StaburovAlexey/router-manager/releases/latest/download/router-manager-linux-${ARCH}"
chmod +x router-manager
sudo install -m 755 router-manager /usr/local/sbin/router-manager
```

Проверьте, что команда установилась:

```bash
router-manager --version
```

## Первый Запуск

Запустите приложение от администратора:

```bash
sudo router-manager
```

При первом запуске Router Manager:

- проверит систему;
- установит нужные пакеты;
- подготовит Wi-Fi точку доступа;
- попросит данные входного и выходных серверов;
- сохранит настройки;
- откроет меню управления.

Если мастер настройки был прерван, просто снова запустите:

```bash
sudo router-manager
```

Приложение продолжит с уже сохранёнными значениями.

## Что Подготовить Заранее

Перед первым запуском удобно иметь под рукой:

- мини-ПК с Ubuntu/Debian;
- подключение мини-ПК к интернету по кабелю или отдельному адаптеру;
- Wi-Fi адаптер для раздачи сети;
- IP, SSH-пользователя, SSH-порт и пароль или ключи для серверов;
- порт подключения для входного и выходных серверов, обычно `443`.

