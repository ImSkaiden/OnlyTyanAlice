#!/bin/sh
# ==============================================================================
# 🔄 Quasar Bridge Client Uninstaller
# Возвращает Яндекс Станцию в оригинальное заводское состояние
# ==============================================================================
set -e

echo "=== Удаление Quasar Bridge и возврат к оригинальному Яндексу ==="

if [ "$(id -u)" -ne 0 ]; then
    echo "❌ ОШИБКА: Запустите от root!" >&2
    exit 1
fi

mount -o remount,rw / 2>/dev/null || true

# Остановка поллера
/etc/init.d/S98station_poller stop 2>/dev/null || true
rm -f /etc/init.d/S98station_poller /data/opt/bin/station_poller.sh

# Восстановление quasar.cfg
BACKUP_DIR="/data/opt/backup"
if [ -f "$BACKUP_DIR/quasar.cfg.orig" ]; then
    cp -af "$BACKUP_DIR/quasar.cfg.orig" /system/vendor/quasar/quasar.cfg
    echo "✅ Восстановлен оригинальный /system/vendor/quasar/quasar.cfg"
else
    # Если бэкапа нет, возвращаем дефолтный URL
    sed -i 's|"uniProxyUrl": *"[^"]*"|"uniProxyUrl": "wss://uniproxy.alice.yandex.net/uni.ws"|g' /system/vendor/quasar/quasar.cfg
    sed -i 's|"wsUseZstd": *false|"wsUseZstd": true|g' /system/vendor/quasar/quasar.cfg
    echo "✅ Возвращен стандартный uniproxy.alice.yandex.net в quasar.cfg"
fi

# Восстановление OTA ключей
if [ -f "$BACKUP_DIR/ota_script.pub.orig" ]; then
    cp -af "$BACKUP_DIR/ota_script.pub.orig" /system/vendor/quasar/ota_script.pub
    echo "✅ Восстановлен ключ OTA"
fi

# Восстановление /etc/hosts
sed -i '/quasar.s3.yandex.net/d' /etc/hosts 2>/dev/null || true
sed -i '/clck.yandex.net/d' /etc/hosts 2>/dev/null || true
sed -i '/appmetrica.yandex.net/d' /etc/hosts 2>/dev/null || true

mount -o remount,ro / 2>/dev/null || true

# Перезапуск
killall -9 maind 2>/dev/null || true
echo "✅ Quasar перезапущен. Колонка подключена напрямую к Яндексу."
