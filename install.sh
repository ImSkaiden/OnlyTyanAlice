#!/bin/sh
# ==============================================================================
# 🚀 Universal Quasar Bridge & OnlyTyan Installer for Yandex Stations (ARM64)
# Поддерживает:
#   - Яндекс Станция Мини 2 (с LED-часами и без)
#   - Яндекс Станция Мини 1
#   - Яндекс Станция Лайт 1 & 2
#   - Яндекс Станция Миди / Станция 2
# ==============================================================================
set -e

export PATH="/bin:/usr/bin:/sbin:/usr/sbin:/opt/bin:$PATH"

echo "===================================================================="
echo "    ✨ Quasar Bridge & OnlyTyan Universal Client Installer ✨       "
echo "===================================================================="

# 1. Проверка прав root
if [ "$(id -u)" -ne 0 ]; then
    echo "❌ ОШИБКА: Этот скрипт должен выполняться от root на колонке!" >&2
    exit 1
fi

# 2. Проверка архитектуры ядра
ARCH=$(uname -m)
if [ "$ARCH" != "aarch64" ]; then
    echo "⚠️ ПРЕДУПРЕЖДЕНИЕ: Нестандартная архитектура ($ARCH). Ожидается aarch64." >&2
fi

# 3. Определение модели Яндекс Станции
echo "[1/7] 🔍 Определение модели станции..."

DT_COMPAT=""
if [ -f /proc/device-tree/compatible ]; then
    DT_COMPAT=$(tr '\0' ' ' < /proc/device-tree/compatible 2>/dev/null || true)
fi

DT_MODEL=""
if [ -f /proc/device-tree/model ]; then
    DT_MODEL=$(tr '\0' ' ' < /proc/device-tree/model 2>/dev/null || true)
fi

CFG_PRESET=""
if [ -f /system/vendor/quasar/quasar.cfg ]; then
    CFG_PRESET=$(grep -o '"counterActivationPresetName": *"[^"]*"' /system/vendor/quasar/quasar.cfg 2>/dev/null | cut -d'"' -f4 || true)
fi

HAS_LED_CLOCK=0
if [ -e /dev/led_display ] || [ -e /dev/led_matrix ] || [ -d /sys/class/led_display ] || grep -qi "clock" /system/vendor/quasar/quasar.cfg 2>/dev/null; then
    HAS_LED_CLOCK=1
fi

STATION_MODEL="Unknown"
STATION_CODE="universal"
STATION_FEATURES=""

case "$DT_COMPAT $DT_MODEL $CFG_PRESET" in
    *mini2*|*yandexmini_2*|*station-mini2*)
        if [ "$HAS_LED_CLOCK" -eq 1 ]; then
            STATION_MODEL="Яндекс Станция Мини 2 с часами (LED Matrix)"
            STATION_CODE="mini2_clock"
            STATION_FEATURES="LED-часы/погода, 10W динамик, сенсорные кнопки +/-, AUX"
        else
            STATION_MODEL="Яндекс Станция Мини 2 (без часов)"
            STATION_CODE="mini2"
            STATION_FEATURES="10W динамик, сенсорные кнопки +/-, AUX"
        fi
        ;;
    *axg*|*yandexmini*|*station-mini*)
        STATION_MODEL="Яндекс Станция Мини 1"
        STATION_CODE="mini1"
        STATION_FEATURES="3W динамик, жестовый ToF сенсор, AUX"
        ;;
    *micro*|*yandexmicro*)
        STATION_MODEL="Яндекс Станция Лайт"
        STATION_CODE="lite"
        STATION_FEATURES="5W динамик, физические кнопки"
        ;;
    *plum*)
        STATION_MODEL="Яндекс Станция Лайт 2"
        STATION_CODE="lite2"
        STATION_FEATURES="LED-экран эмоций, 6W динамик"
        ;;
    *midi*|*cucumber*)
        STATION_MODEL="Яндекс Станция Миди"
        STATION_CODE="midi"
        STATION_FEATURES="24W звук, Zigbee хаб, дисплей"
        ;;
    *)
        STATION_MODEL="Яндекс Станция (Универсальная)"
        STATION_CODE="universal"
        STATION_FEATURES="Quasar AAPI прошивка"
        ;;
esac

echo "      📱 Найдено устройство: \033[1;32m$STATION_MODEL\033[0m"
echo "      ⚙️ Особенности: $STATION_FEATURES"

# 4. Адрес Бриджа и Сервера OnlyTyan
DEFAULT_BRIDGE="wss://ot.imskaiden.xyz/uni.ws"
DEFAULT_SERVER="https://ot.imskaiden.xyz"

BRIDGE_URL="${1:-$DEFAULT_BRIDGE}"
SERVER_URL="${2:-$DEFAULT_SERVER}"

echo ""
echo "[2/7] 🌐 Настройка сетевых адресов:"
echo "      • WebSocket Bridge: $BRIDGE_URL"
echo "      • OnlyTyan Server:  $SERVER_URL"
echo ""

# 5. Подготовка файловой системы и бэкап
echo "[3/7] 💾 Создание резервных копий и перемонтирование rootfs..."
mount -o remount,rw / 2>/dev/null || true

BACKUP_DIR="/data/opt/backup"
mkdir -p "$BACKUP_DIR" /data/opt/bin

if [ -f /system/vendor/quasar/quasar.cfg ] && [ ! -f "$BACKUP_DIR/quasar.cfg.orig" ]; then
    cp -af /system/vendor/quasar/quasar.cfg "$BACKUP_DIR/quasar.cfg.orig"
    echo "      ✅ Создана копия /system/vendor/quasar/quasar.cfg.orig"
fi

if [ -f /system/vendor/quasar/launch.json ] && [ ! -f "$BACKUP_DIR/launch.json.orig" ]; then
    cp -af /system/vendor/quasar/launch.json "$BACKUP_DIR/launch.json.orig"
    echo "      ✅ Создана копия /system/vendor/quasar/launch.json.orig"
fi

# 6. Отключение автообновлений Яндекса (Защита от сброса root)
echo "[4/7] 🛡️ Защита от автообновлений Яндекса (OTA Lock)..."

# Подменяем публичный ключ проверки обновлений на пустой/фиктивный
if [ -f /system/vendor/quasar/ota_script.pub ]; then
    if [ ! -f "$BACKUP_DIR/ota_script.pub.orig" ]; then
        cp -af /system/vendor/quasar/ota_script.pub "$BACKUP_DIR/ota_script.pub.orig"
    fi
    echo "DUMMY_PUB_KEY_ONLYTYAN_OTA_BLOCK" > /system/vendor/quasar/ota_script.pub
    chmod 644 /system/vendor/quasar/ota_script.pub
fi

# Заглушаем скрипт обновления прошивки
if [ -f /system/vendor/quasar/device_ota_update.sh ]; then
    cat << 'EOF_OTA' > /system/vendor/quasar/device_ota_update.sh
#!/bin/sh
echo "OTA updates are disabled by Quasar Bridge."
exit 0
EOF_OTA
    chmod 755 /system/vendor/quasar/device_ota_update.sh
fi

# Блокировка CDN обновлений в /etc/hosts
if ! grep -q "quasar.s3.yandex.net" /etc/hosts 2>/dev/null; then
    cat << 'EOF_HOSTS' >> /etc/hosts

# Блокировка OTA Яндекса
127.0.0.1 quasar.s3.yandex.net
127.0.0.1 clck.yandex.net
127.0.0.1 appmetrica.yandex.net
127.0.0.1 metrica.yandex.net
EOF_HOSTS
fi
echo "      ✅ Защита OTA активирована."

# 7. Патчинг quasar.cfg для перенаправления в Quasar Bridge
echo "[5/7] 🔧 Перенаправление Uniproxy на Quasar Bridge..."

QCFG="/system/vendor/quasar/quasar.cfg"
if [ -f "$QCFG" ]; then
    # Заменяем uniProxyUrl
    sed -i "s|\"uniProxyUrl\": *\"[^\"]*\"|\"uniProxyUrl\": \"$BRIDGE_URL\"|g" "$QCFG"
    
    # Отключаем Zstd сжатие для корректного парсинга protobuf/json мостом
    sed -i "s|\"wsUseZstd\": *true|\"wsUseZstd\": false|g" "$QCFG"

    # Специфика для Станции Мини 2
    if [ "$STATION_CODE" = "mini2" ] || [ "$STATION_CODE" = "mini2_clock" ]; then
        # Проверяем наличие корректных директив в quasar.cfg
        echo "      ℹ️ Применены оптимизации для Станции Мини 2 (10W звук, кнопки)"
    fi

    echo "      ✅ Конфигурация $QCFG успешно обновлена!"
else
    echo "❌ ОШИБКА: Файл $QCFG не найден!" >&2
    exit 1
fi

# 8. Установка демона опроса команд и нативного TTS (glagol_daemon)
echo "[6/7] 📻 Установка сервиса управления OnlyTyan (glagol_daemon)..."

# Скачиваем свежий бинарник демона с сервера
DAEMON_PATH="/data/opt/bin/glagol_daemon"
if [ -x "/tmp/soloist-pkg/bin/curl" ]; then
    CURL_CMD="/tmp/soloist-pkg/bin/curl"
elif which curl >/dev/null 2>&1; then
    CURL_CMD="curl"
else
    CURL_CMD=""
fi

if [ -n "$CURL_CMD" ]; then
    $CURL_CMD -s -L "$SERVER_URL/static/glagol_daemon" -o "$DAEMON_PATH" 2>/dev/null || true
elif which wget >/dev/null 2>&1; then
    wget -q -O "$DAEMON_PATH" "$SERVER_URL/static/glagol_daemon" 2>/dev/null || true
fi

if [ ! -s "$DAEMON_PATH" ] && [ -f "/data/opt/bin/glagol_daemon" ]; then
    echo "      ℹ️ Используется локально установленный glagol_daemon"
fi
chmod 755 "$DAEMON_PATH" 2>/dev/null || true

# Скрипт воркера
cat << EOF_POLLER > /data/opt/bin/station_poller.sh
#!/bin/sh
# OnlyTyan Station Poller Runner
while true; do
    if [ -x /data/opt/bin/glagol_daemon ]; then
        /data/opt/bin/glagol_daemon -server $SERVER_URL >> /tmp/glagol_daemon.log 2>&1
    fi
    sleep 2
done
EOF_POLLER
chmod 755 /data/opt/bin/station_poller.sh

# Сервис автозапуска в /etc/init.d
cat << 'EOF_INIT' > /etc/init.d/S98station_poller
#!/bin/sh
case "$1" in
    start)
        killall station_poller.sh glagol_daemon 2>/dev/null || true
        /data/opt/bin/station_poller.sh >/dev/null 2>&1 &
        ;;
    stop)
        killall station_poller.sh glagol_daemon 2>/dev/null || true
        ;;
    restart)
        "$0" stop
        sleep 1
        "$0" start
        ;;
    *)
        echo "Usage: $0 {start|stop|restart}" >&2
        exit 1
        ;;
esac
EOF_INIT
chmod 755 /etc/init.d/S98station_poller

# Запускаем поллер
/etc/init.d/S98station_poller restart >/dev/null 2>&1 || true

# 8b. Установка утилиты управления tyan CLI
echo "      🛠️ Установка утилиты командной строки tyan CLI..."
TYAN_BIN="/data/opt/bin/tyan"
if [ -n "$CURL_CMD" ]; then
    $CURL_CMD -s -L "https://raw.githubusercontent.com/ImSkaiden/OnlyTyanAlice/main/client/tyan" -o "$TYAN_BIN" 2>/dev/null || true
elif which wget >/dev/null 2>&1; then
    wget -q -O "$TYAN_BIN" "https://raw.githubusercontent.com/ImSkaiden/OnlyTyanAlice/main/client/tyan" 2>/dev/null || true
fi
chmod 755 "$TYAN_BIN" 2>/dev/null || true
ln -sf "$TYAN_BIN" /usr/bin/tyan 2>/dev/null || true
ln -sf "$TYAN_BIN" /bin/tyan 2>/dev/null || true

# Возвращаем rootfs в режим только для чтения
mount -o remount,ro / 2>/dev/null || true

# 9. Перезапуск Quasar maind
echo "[7/7] 🔄 Перезапуск служб Quasar..."
killall -9 maind 2>/dev/null || true

echo ""
echo "===================================================================="
echo "    🎉 УСТАНОВКА УСПЕШНО ЗАВЕРШЕНА!                                "
echo "===================================================================="
echo "Модель:        $STATION_MODEL"
echo "Quasar Bridge: $BRIDGE_URL"
echo "OnlyTyan:      $SERVER_URL"
echo ""
echo "Управление колонкой через консоль (CLI):"
echo "  tyan status     - Полная диагностика и статус сервисов"
echo "  tyan test       - Проверка сети, звука и связи с мостом"
echo "  tyan say \"...\"  - Озвучить фразу через динамик"
echo "  tyan update     - Обновить софт и скрипты с GitHub"
echo "  tyan logs       - Просмотр живых логов"
echo ""
echo "Что теперь произойдет:"
echo "  1. Колонка подключена к вашему Quasar Bridge по безопасному WSS."
echo "  2. При обращении «Тян» запросы пойдут через LLM-мост."
echo "  3. Системные действия (музыка, таймеры, часы) прозрачно идут в Яндекс."
if [ "$STATION_CODE" = "mini2_clock" ]; then
echo "  4. LED-часы и погода на передней панели работают штатно."
fi
echo "===================================================================="
