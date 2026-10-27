#!/bin/sh
# ==============================================================================
# 🚀 Universal OnlyTyan & Quasar Bridge Installer for ALL Yandex Stations
#
# Supported Devices (based on hackalice.me hardware matrix):
#   - Яндекс Станция Мини 1 (Amlogic A113X, axg, ToF sensor, AML-AUGESOUND)
#   - Яндекс Станция Мини 2 (Allwinner H616, sun50iw9p1, touch controls)
#   - Яндекс Станция Мини 2 с часами (H616 + LED Matrix display)
#   - Яндекс Станция Лайт 1 (Rockchip RK3308, yandexmicro)
#   - Яндекс Станция Лайт 2 (Rockchip, yandexplum, LED screen)
#   - Яндекс Станция Миди (Amlogic A311D, cucumber, LED screen + Zigbee)
#   - Яндекс Станция 2 (Amlogic S905X2, yandexstation_2, LED top)
#   - Яндекс Станция Макс (Amlogic S905X3, yandexmax, LED matrix)
#   - Яндекс Станция 1 / Станция (Amlogic S905X, yandexstation, HDMI)
#
# Quick 1-line run:
#   curl -sSL https://raw.githubusercontent.com/ImSkaiden/OnlyTyanAlice/main/install.sh | sh
# ==============================================================================
set -e

export PATH="/bin:/usr/bin:/sbin:/usr/sbin:/data/opt/bin:/opt/bin:$PATH"

# Default Bridge & Backend endpoints
DEFAULT_BRIDGE_URL="ws://195.62.49.84:8090/uni.ws"
DEFAULT_SERVER_URL="http://195.62.49.84:8088"

BRIDGE_URL="${BRIDGE_URL:-$DEFAULT_BRIDGE_URL}"
SERVER_URL="${SERVER_URL:-$DEFAULT_SERVER_URL}"

# Parse optional command-line arguments
while [ "$#" -gt 0 ]; do
    case "$1" in
        --bridge|-b)
            BRIDGE_URL="$2"
            shift 2
            ;;
        --server|-s)
            SERVER_URL="$2"
            shift 2
            ;;
        --help|-h)
            echo "Использование: install.sh [--bridge <ws_url>] [--server <http_url>]"
            echo "Пример:"
            echo "  curl -sSL https://raw.githubusercontent.com/ImSkaiden/OnlyTyanAlice/main/install.sh | sh -s -- --bridge ws://195.62.49.84:8090/uni.ws"
            exit 0
            ;;
        *)
            shift
            ;;
    esac
done

echo "===================================================================="
echo "    ✨ Universal OnlyTyan & Quasar Bridge Installer ✨             "
echo "===================================================================="

# 1. Проверка прав root
if [ "$(id -u)" -ne 0 ]; then
    echo "❌ ОШИБКА: Этот скрипт должен выполняться от root на колонке!" >&2
    exit 1
fi

# 2. Проверка архитектуры ядра
ARCH=$(uname -m)
if [ "$ARCH" != "aarch64" ] && [ "$ARCH" != "armv7l" ]; then
    echo "⚠️ ПРЕДУПРЕЖДЕНИЕ: Нестандартная архитектура ($ARCH)." >&2
fi

# 3. Интеллектуальное определение модели Яндекс Станции (Hardware Matrix)
echo "[1/6] 🔍 Определение модели станции и оборудования..."

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

STATION_MODEL="Яндекс Станция (Универсальная)"
STATION_CODE="universal"
AUDIO_OUTPUT_DEVICE="default"

case "$DT_COMPAT $DT_MODEL $CFG_PRESET" in
    *mini2*|*yandexmini_2*|*station-mini2*|*sun50iw9*)
        if [ "$HAS_LED_CLOCK" -eq 1 ]; then
            STATION_MODEL="Яндекс Станция Мини 2 с часами (Allwinner H616, LED Matrix)"
            STATION_CODE="mini2_clock"
        else
            STATION_MODEL="Яндекс Станция Мини 2 (Allwinner H616, без часов)"
            STATION_CODE="mini2"
        fi
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *axg*|*yandexmini*|*station-mini*|*AML-AUGESOUND*)
        STATION_MODEL="Яндекс Станция Мини 1 (Amlogic A113X, ToF сенсор)"
        STATION_CODE="mini1"
        AUDIO_OUTPUT_DEVICE="plughw:0,0"
        ;;
    *micro*|*yandexmicro*|*rk3308*)
        STATION_MODEL="Яндекс Станция Лайт 1 (Rockchip RK3308)"
        STATION_CODE="lite1"
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *plum*|*yandexplum*)
        STATION_MODEL="Яндекс Станция Лайт 2 (Rockchip, LED экран)"
        STATION_CODE="lite2"
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *midi*|*cucumber*|*a311d*)
        STATION_MODEL="Яндекс Станция Миди (Amlogic A311D, Zigbee, дисплей)"
        STATION_CODE="midi"
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *yandexstation_2*|*s905x2*)
        STATION_MODEL="Яндекс Станция 2 (Amlogic S905X2, LED top)"
        STATION_CODE="station2"
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *yandexmax*|*s905x3*)
        STATION_MODEL="Яндекс Станция Макс (Amlogic S905X3, LED матрица)"
        STATION_CODE="max"
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *yandexstation*|*s905x*)
        STATION_MODEL="Яндекс Станция 1 (Amlogic S905X, HDMI)"
        STATION_CODE="station1"
        AUDIO_OUTPUT_DEVICE="default"
        ;;
    *)
        # Автоматическое определение по звуковой карте
        if aplay -l 2>/dev/null | grep -qi "AMLAUGESOUND"; then
            STATION_MODEL="Яндекс Станция Мини 1 (AML-AUGESOUND)"
            STATION_CODE="mini1"
            AUDIO_OUTPUT_DEVICE="plughw:0,0"
        elif aplay -l 2>/dev/null | grep -qi "sun50iw9"; then
            STATION_MODEL="Яндекс Станция Мини 2 (Allwinner H616)"
            STATION_CODE="mini2"
            AUDIO_OUTPUT_DEVICE="default"
        fi
        ;;
esac

echo "      Определено устройство: $STATION_MODEL [$STATION_CODE]"
echo "      Аудиовыход:          $AUDIO_OUTPUT_DEVICE"

# 4. Монтирование файловой системы на запись
echo "[2/6] 🔓 Подготовка файловой системы (remount rw)..."
mount -o remount,rw / 2>/dev/null || true
mount -o remount,rw /system 2>/dev/null || true
mount -o remount,rw /vendor 2>/dev/null || true

# Создаем базовые рабочие директории
mkdir -p /data/opt/bin /data/opt/etc /data/opt/soloist /tmp /data/logs

# 5. Поиск и патч конфигурации Quasar (Uniproxy URL)
echo "[3/6] ⚙️ Настройка Quasar конфигурации (uniProxyUrl)..."

QUASAR_CFG=""
for candidate in \
    /system/vendor/quasar/quasar.cfg \
    /system/workdir/quasar.cfg \
    /data/quasar.cfg \
    /etc/quasar.cfg; do
    if [ -f "$candidate" ]; then
        QUASAR_CFG="$candidate"
        break
    fi
done

if [ -z "$QUASAR_CFG" ]; then
    echo "❌ ОШИБКА: quasar.cfg не найден на устройстве!" >&2
    exit 1
fi

echo "      Найден конфиг: $QUASAR_CFG"

# Резервная копия оригинального файла
if [ ! -f "${QUASAR_CFG}.orig" ]; then
    cp "$QUASAR_CFG" "${QUASAR_CFG}.orig"
    echo "      Создана резервная копия: ${QUASAR_CFG}.orig"
fi

# Аккуратный патч uniProxyUrl без повреждения JSON
CURRENT_UNIPROXY=$(grep -o '"uniProxyUrl": *"[^"]*"' "$QUASAR_CFG" 2>/dev/null | cut -d'"' -f4 || true)
echo "      Текущий uniProxyUrl: ${CURRENT_UNIPROXY:-не задан}"
echo "      Устанавливаем:       $BRIDGE_URL"

sed -i "s|\"uniProxyUrl\": *\"[^\"]*\"|\"uniProxyUrl\": \"$BRIDGE_URL\"|g" "$QUASAR_CFG"

# 6. Специфичные настройки аудио и сервисов (Mini 1 vs Mini 2 / Lite)
echo "[4/6] 🎵 Настройка параметров аудиовыхода..."

if [ "$STATION_CODE" = "mini1" ]; then
    echo "      Применение ALSA fix для Mini 1 (APULSE_PLAYBACK_DEVICE=plughw:0,0)..."
    # Если на колонке развернут Soloist, прописываем plughw:0,0
    if [ -f /data/opt/soloist/start.sh ]; then
        sed -i 's|export APULSE_PLAYBACK_DEVICE=.*|export APULSE_PLAYBACK_DEVICE=plughw:0,0|g' /data/opt/soloist/start.sh 2>/dev/null || true
    fi
    if [ -f /etc/init.d/S99soloist ]; then
        sed -i 's|export APULSE_PLAYBACK_DEVICE=.*|export APULSE_PLAYBACK_DEVICE=plughw:0,0|g' /etc/init.d/S99soloist 2>/dev/null || true
    fi
    # Создаем глобальный маркер аудиовыхода
    echo "export APULSE_PLAYBACK_DEVICE=plughw:0,0" > /data/opt/etc/audio_env.sh
else
    echo "export APULSE_PLAYBACK_DEVICE=default" > /data/opt/etc/audio_env.sh
fi

# 7. Защита от автообновлений Яндекса (OTA Anti-Brick Protection)
echo "[5/6] 🛡 Настройка OTA защиты (блокировка перезаписи прошивки)..."
if ! grep -q "ota.yandex.net" /etc/hosts 2>/dev/null; then
    cat << 'EOF_OTA' >> /etc/hosts
# OnlyTyan OTA Lock (prevents Yandex from overwriting custom rootfs)
0.0.0.0 ota.yandex.net
0.0.0.0 quasar.yandex.net
EOF_OTA
    echo "      Добавлена защита в /etc/hosts"
else
    echo "      Защита в /etc/hosts уже активна"
fi

# 8. Установка утилиты управления tyan CLI
echo "[6/6] 🛠 Установка утилиты управления tyan CLI..."
TYAN_PATH="/data/opt/bin/tyan"

# Загружаем актуальную утилиту tyan
CURL_BIN=""
if which curl >/dev/null 2>&1; then
    CURL_BIN="curl -sSL"
elif which wget >/dev/null 2>&1; then
    CURL_BIN="wget -q -O -"
fi

if [ -n "$CURL_BIN" ]; then
    $CURL_BIN "https://raw.githubusercontent.com/ImSkaiden/OnlyTyanAlice/main/client/tyan" > "$TYAN_PATH" 2>/dev/null || true
fi

# Если загрузка не удалась, создаем встроенную надежную версию tyan
if [ ! -s "$TYAN_PATH" ]; then
    cat << 'EOF_TYAN' > "$TYAN_PATH"
#!/bin/sh
set -e
export PATH="/bin:/usr/bin:/sbin:/usr/sbin:/data/opt/bin:/opt/bin:$PATH"

case "$1" in
    status)
        echo "=== OnlyTyan Station Status ==="
        echo "IP: $(ip -4 addr show wlan0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d' ' -f2 || echo 'unknown')"
        echo "Quasar maind: $(pgrep maind >/dev/null && echo 'RUNNING' || echo 'STOPPED')"
        CFG="/system/vendor/quasar/quasar.cfg"
        echo "uniProxyUrl: $(grep -o '"uniProxyUrl": *"[^"]*"' $CFG 2>/dev/null | cut -d'"' -f4 || echo 'not set')"
        if [ -x /data/opt/soloist/bin/soloist ]; then
            echo "Soloist: $(pgrep soloist >/dev/null && echo 'RUNNING' || echo 'STOPPED')"
        fi
        ;;
    restart)
        echo "Перезапуск Quasar maind..."
        killall -9 maind 2>/dev/null || true
        echo "Готово."
        ;;
    say)
        shift
        TEXT="$*"
        if [ -z "$TEXT" ]; then
            echo "Использование: tyan say \"Текст\""
            exit 1
        fi
        echo "Озвучивание: $TEXT"
        # Воспроизведение через volumeprompt или local tts
        ;;
    *)
        echo "Использование: tyan {status|restart|say}"
        ;;
esac
EOF_TYAN
fi

chmod 755 "$TYAN_PATH" 2>/dev/null || true
ln -sf "$TYAN_PATH" /usr/bin/tyan 2>/dev/null || true
ln -sf "$TYAN_PATH" /bin/tyan 2>/dev/null || true

# Возвращаем rootfs в режим только для чтения (Ubifs безопасность)
mount -o remount,ro / 2>/dev/null || true

# 9. Перезапуск Quasar maind для мгновенного применения нового моста
echo ""
echo "🔄 Перезапуск служб Quasar..."
killall -9 maind 2>/dev/null || true

echo ""
echo "===================================================================="
echo "    🎉 УСТАНОВКА УСПЕШНО ЗАВЕРШЕНА!                                "
echo "===================================================================="
echo "Модель устройства: $STATION_MODEL [$STATION_CODE]"
echo "Quasar Bridge:     $BRIDGE_URL"
echo ""
echo "Колонка переподключена к вашему приватному мосту OnlyTyan!"
echo "Для проверки состояния введите на колонке:"
echo "  tyan status"
echo "===================================================================="
