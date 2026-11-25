# Mobilix Launcher PC

Основано на [Mobilix Launcher for Minecraft](https://github.com/Kumolix/Mobilix-Launcher-for-Minecraft), автор Kumolix.
Based on Mobilix Launcher for Minecraft by Kumolix.

Лаунчер Minecraft: Java Edition для Windows 10/11 в оформлении мобильного Mobilix, горизонтальный вид. Один .exe, без установки.

## Что внутри
- Vanilla и Fabric, любые версии из манифеста Mojang, скачивание библиотек и ресурсов в несколько потоков с проверкой sha1.
- Java нужной версии находится сама или скачивается (Eclipse Temurin).
- Каталог модов, ресурспаков и шейдеров (Modrinth), 3D-скин, лог внутри лаунчера, настройки памяти и JVM, русский и английский язык.
- Оформления Графит и Классика (зелёное), тёмная и светлая темы, нижнее и боковое меню.

## Отличия от мобильной версии
1. «Играть» и версия стоят справа внизу.
2. Сверху слева кнопка выхода, рядом с ней «Лог».
3. Лог открывается в окне поверх скина.
4. Боковое меню выезжает при наведении мыши на левый край и убирается, когда мышь ушла. У нижнего меню иконки при наведении увеличиваются и выходят за края панели.

## Запуск
Скачайте `Mobilix-Launcher.exe` из Actions (артефакт `Mobilix-Launcher-PC`) и запустите. Окно рисуется встроенным Microsoft Edge (WebView2/Edge есть в Windows 10 и 11) или Chrome. Данные лежат в `%AppData%\Mobilix`.

Вход через Microsoft пока не поддерживается: игра запускается в офлайн-режиме с ником.

## Сборка
```
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H=windowsgui" -o Mobilix-Launcher.exe .
```
