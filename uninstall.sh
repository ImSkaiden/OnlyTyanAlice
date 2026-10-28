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
mount -o remount,rw /system 2>/dev/null || true
mount -o remount,rw /vendor 2>/dev/null || true

# Остановка демонов и поллеров
/etc/init.d/S99glagol stop 2>/dev/null || true
killall -9 glagol_daemon 2>/dev/null || true
/etc/init.d/S98station_poller stop 2>/dev/null || true
rm -f /etc/init.d/S99glagol /etc/init.d/S98station_poller /data/opt/bin/glagol_daemon /data/opt/bin/station_poller.sh /data/opt/bin/tyan /usr/bin/tyan /bin/tyan /data/opt/etc/device_id /data/opt/etc/audio_env.sh

# Восстановление quasar.cfg
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

BACKUP_DIR="/data/opt/backup"
if [ -n "$QUASAR_CFG" ] && [ -f "${QUASAR_CFG}.orig" ]; then
    cp -af "${QUASAR_CFG}.orig" "$QUASAR_CFG"
    echo "✅ Восстановлен оригинальный $QUASAR_CFG"
elif [ -n "$QUASAR_CFG" ] && [ -f "$BACKUP_DIR/quasar.cfg.orig" ]; then
    cp -af "$BACKUP_DIR/quasar.cfg.orig" "$QUASAR_CFG"
    echo "✅ Восстановлен оригинальный $QUASAR_CFG из резервной копии"
elif [ -n "$QUASAR_CFG" ]; then
    # Если бэкапа нет, возвращаем дефолтный URL
    sed -i 's|"uniProxyUrl": *"[^"]*"|"uniProxyUrl": "wss://uniproxy.alice.yandex.net/uni.ws"|g' "$QUASAR_CFG"
    sed -i 's|"wsUseZstd": *false|"wsUseZstd": true|g' "$QUASAR_CFG"
    echo "✅ Возвращен стандартный uniproxy.alice.yandex.net в $QUASAR_CFG"
fi

# Восстановление OTA ключей
if [ -f "$BACKUP_DIR/ota_script.pub.orig" ]; then
    cp -af "$BACKUP_DIR/ota_script.pub.orig" /system/vendor/quasar/ota_script.pub
    echo "✅ Восстановлен ключ OTA"
fi

# Восстановление /etc/hosts
sed -i '/ota.yandex.net/d' /etc/hosts 2>/dev/null || true
sed -i '/quasar.s3.yandex.net/d' /etc/hosts 2>/dev/null || true
sed -i '/clck.yandex.net/d' /etc/hosts 2>/dev/null || true
sed -i '/appmetrica.yandex.net/d' /etc/hosts 2>/dev/null || true

sync
mount -o remount,ro /system 2>/dev/null || true
mount -o remount,ro /vendor 2>/dev/null || true
mount -o remount,ro / 2>/dev/null || true

# Перезапуск
killall -9 maind 2>/dev/null || true
echo "✅ Quasar перезапущен. Колонка подключена напрямую к Яндексу."
