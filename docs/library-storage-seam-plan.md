# Library storage seam: контекст и согласованный план

Historical plan for the SQLite storage seam. PostgreSQL runtime configuration,
migration ownership, and deployment requirements now follow [README](../README.md)
and [the PostgreSQL follow-ups](../tasks/postgresql-contract-follow-ups.md).

## Статус документа

Этот документ фиксирует решения, согласованные перед реализацией. Код по этому плану ещё не менялся. Документ предназначен как самостоятельный контекст для агента, который будет выполнять работу.

## Цель

Выделить заменяемый persistence seam для библиотечного домена (`Feed`, `Item`, RSS/visibility), сохранив SQLite единственной реализацией на этом этапе. Рефакторинг должен подготовить последующее добавление PostgreSQL-адаптера без повторного переписывания handlers и доменных правил.

Одновременно нужно:

- централизовать ownership, not-found и mutation semantics;
- заменить пользовательский hide/tombstone механизм настоящим удалением items;
- сделать удаление feed атомарным и каскадным на его items;
- перейти от package globals и открытия SQLite на каждый запрос к одному внедряемому `*sql.DB`;
- зафиксировать поведение contract-тестами, пригодными для будущего PostgreSQL-адаптера.

## Не входит в эту работу

- PostgreSQL-адаптер и переключение production на PostgreSQL;
- перенос auth, passwords, WebAuthn и refresh tokens на repository interfaces;
- полный REST redesign существующего `/api/v1`;
- переименование существующих list/add/get/RSS endpoints;
- максимальный предел для pagination `limit`.

Auth storage временно продолжит использовать legacy `internal/db`. В коде рядом с его конфигурацией и в composition root нужно оставить TODO: перевести auth repositories на constructor injection в отдельной работе.

## Проблемы текущей реализации

- Library handlers напрямую импортируют package-global `internal/db`.
- Каждый DB-метод открывает и закрывает собственный SQLite handle.
- `sql.ErrNoRows` протекает до HTTP-слоя.
- Ownership распределён между handlers и SQL-запросами.
- Удаление feed выполняет lookup, проверку owner и delete раздельно, без общей транзакции.
- `deleted_items` не имеет PK, unique или foreign keys и принимает чужие/несуществующие item IDs.
- Скрытый item исключается из списков и RSS, но доступен по прямому ID.
- RSS/list не имеют гарантированного порядка, тогда как pagination использует `id DESC`.
- В тестах нет общего repository contract suite.

## Целевая архитектура

```text
HTTP handlers
    -> library.Service
        -> FeedRepository
        -> ItemRepository
            -> storage/sqlite adapters
                -> shared *sql.DB

/add_lazy handler
    -> FeedResolver (URL -> CreateFeedInput)
    -> library.Service.AddFeed
```

### Пакеты

- `internal/library`
  - доменные типы `Feed`, `Item`, `ItemDetails`, `Page`, `CreateFeedInput`;
  - `Service`;
  - `FeedRepository` и `ItemRepository`;
  - доменная ошибка `ErrNotFound`.
- `internal/storage/sqlite`
  - SQLite-реализации library repositories;
  - library-specific schema/migration code, если это не создаёт ненужного дублирования общего migration entrypoint.
- `internal/handlers`
  - `LibraryHandler`, принимающий узкий `LibraryService` interface и отдельный `FeedResolver`;
  - HTTP parsing, authentication context, DTO mapping и response encoding.
- `internal/db`
  - временно остаётся legacy persistence для auth;
  - использует тот же долгоживущий `*sql.DB`, но пока не получает полноценный repository seam.

Доменные типы не должны содержать JSON tags. Текущий внешний JSON-контракт сохраняется через handler DTO, включая существующее имя поля `userid`.

## Согласованные repository contracts

Базовая форма интерфейсов:

```go
type FeedRepository interface {
    List(ctx context.Context, userID int) ([]Feed, error)
    Add(ctx context.Context, userID int, input CreateFeedInput) (Feed, error)
    Delete(ctx context.Context, userID, feedID int) error
}

type ItemRepository interface {
    Get(ctx context.Context, userID, itemID int) (ItemDetails, error)
    List(ctx context.Context, userID int) ([]Item, error)
    ListPage(ctx context.Context, userID int, cursor *int, limit int) ([]Item, error)
    Delete(ctx context.Context, userID, itemID int) error
}
```

Контракт `FeedRepository.Delete` включает атомарное удаление принадлежащего пользователю feed и всех его items. SQLite-адаптер реализует это явной транзакцией, а не schema rebuild с `ON DELETE CASCADE` на этом этапе.

Все repository-методы:

- принимают `context.Context`;
- применяют owner scope внутри SQL;
- не возвращают `sql.ErrNoRows` наружу;
- возвращают/оборачивают `library.ErrNotFound` для отсутствующего или чужого ресурса;
- не раскрывают различие между чужим и несуществующим ID.

`CreateFeedInput` содержит только `Title`, `Type`, `URL`. Клиент не может назначать `ID` или `UserID`.

## Library service

`library.Service` использует раздельные `FeedRepository` и `ItemRepository` и предоставляет handlers операции для:

- получения списка feeds;
- добавления feed;
- удаления feed;
- получения `ItemDetails{Item, Feed}`;
- получения всех items для RSS;
- получения страницы items;
- удаления item.

Pagination shaping принадлежит service:

- handler разбирает `limit` и `cursor`;
- default `limit` остаётся `100`;
- новый maximum не вводится;
- repository возвращает строки в `id DESC`;
- service формирует `Page{Items, NextCursor}`;
- пустые коллекции нормализуются в пустой slice, чтобы JSON был `[]`, а не `null`.

Все item list/read paths должны иметь детерминированный порядок `id DESC`, включая RSS.

## FeedResolver и `/add_lazy`

Преобразование URL в полный payload остаётся отдельным сервисом `FeedResolver`. Оно не входит в repository и не должно смешиваться с SQLite.

`/add_lazy` handler оркестрирует два независимых вызова:

1. `FeedResolver` преобразует URL в `CreateFeedInput`.
2. `library.Service.AddFeed` сохраняет feed для authenticated user.

Resolver errors продолжают отображаться как клиентская ошибка согласно текущему API.

## HTTP API

Существующие маршруты и формы ответов сохраняются, кроме явно перечисленных delete-изменений. В частности, не нужно проводить общий REST redesign для `/list`, `/add`, `/add_lazy`, `/item`, `/get_items`, `/feed` и `/feed_types`.

### Новый hard delete item

```http
DELETE /api/v1/items/{id}
```

- authentication обязателен;
- положительный существующий ID, принадлежащий пользователю: физическое удаление и `204 No Content`;
- чужой, отсутствующий или уже удалённый item: `404 Not Found`;
- нечисловой, нулевой или отрицательный path ID: `400 Bad Request`;
- ответ не раскрывает существование чужого item.

### Delete feed

Существующий feed delete endpoint сохраняет свой URL, но меняет mutation semantics:

- owner-scoped delete выполняется одной repository-транзакцией;
- удаляются feed и все связанные items;
- успех: `204 No Content`;
- чужой, отсутствующий или уже удалённый feed: `404 Not Found`.

### Deprecated hide endpoint

Authenticated `POST /api/v1/add_deleted_items` больше не создаёт tombstones и возвращает:

```http
HTTP/1.1 410 Gone
Content-Type: application/json
```

```json
{
  "error": "endpoint_gone",
  "replacement": "DELETE /api/v1/items/{id}"
}
```

Без authentication endpoint по-прежнему возвращает `401`.

Нужно создать/зафиксировать отдельный follow-up на полное удаление route после подтверждения, что старых клиентов больше нет. Конкретная дата сейчас не назначается; до выполнения follow-up route остаётся как `410` compatibility stub.

## Миграция `deleted_items`

Миграция выполняется автоматически при startup по существующему паттерну `MigratePasswords`: идемпотентная Go-функция и одна транзакция.

Алгоритм:

1. Проверить существование таблицы `deleted_items`. На свежей или уже мигрированной базе отсутствие таблицы является успешным no-op.
2. Найти только валидные tombstones, для которых `deleted_items.user_id` совпадает с владельцем feed соответствующего item.
3. Физически удалить только эти items.
4. Tombstones с чужим item ID, отсутствующим item или нарушенной связью не должны приводить к удалению чужих данных.
5. Удалить таблицу `deleted_items` в той же транзакции.
6. Commit только после успешного выполнения всех шагов.

Дубликаты tombstones и уже отсутствующие items должны обрабатываться безопасно. Миграция возвращает статистику как минимум по:

- числу физически удалённых items;
- числу проигнорированных чужих/битых tombstone rows.

Startup логирует эту статистику без раскрытия содержимого записей.

Миграция необратима. README должен явно требовать backup SQLite-файла перед установкой этой версии. Автоматический backup в рамках задачи не создаётся.

После миграции:

- core schema больше не создаёт `deleted_items`;
- item list/get/RSS queries больше не содержат tombstone-фильтры;
- старый endpoint остаётся только статическим authenticated `410` handler.

## DB lifecycle

- Composition root открывает один `*sql.DB` при старте.
- Этот pool передаётся SQLite library repositories.
- Тот же pool временно конфигурирует legacy `internal/db` для auth.
- Legacy DB-функции не должны закрывать общий pool после каждого вызова.
- Library queries используют `QueryContext`, `QueryRowContext`, `ExecContext` и `BeginTx`.
- Временный global auth wiring сопровождается TODO в composition root и рядом с legacy DB configuration API.

Приложение обрабатывает `SIGINT` и `SIGTERM`:

1. прекращает принимать новые запросы;
2. даёт HTTP server до 10 секунд на graceful shutdown;
3. закрывает общий DB pool;
4. корректно обрабатывает `http.ErrServerClosed`.

## Ошибки

- `library.ErrNotFound` — единый доменный sentinel для чужого и отсутствующего library resource.
- Library handlers отображают его в `404`.
- Невалидный пользовательский ввод отображается в `400`.
- Неожиданные repository/service errors логируются и отображаются в `500`.
- Library HTTP error mapping не должен ломать временное legacy-поведение auth, которое пока может использовать SQL-specific errors.

## Тестирование

### Library service unit tests

Использовать fake repositories и проверить:

- pagination (`limit + 1`, обрезание, `NextCursor`);
- нормализацию пустых slices;
- распространение `ErrNotFound` и неожиданных ошибок;
- отсутствие обхода ownership contracts.

### SQLite repository contract suite

Сделать общий suite, который позже можно запустить и для PostgreSQL adapter. Минимальные гарантии:

- Feed создаётся с server-controlled ID и owner.
- Feed list возвращает только feeds пользователя и пустой slice вместо `nil`.
- Item get возвращает `ItemDetails` только владельцу.
- Чужой и отсутствующий item дают `ErrNotFound`.
- Item lists/pages изолированы по owner и отсортированы `id DESC`.
- Cursor pagination не создаёт повторов и пропусков в стабильном наборе данных.
- Delete item физически удаляет только принадлежащий пользователю item.
- Delete feed атомарно удаляет feed и все его items.
- Чужой feed не изменяется.
- Ошибка внутри транзакции приводит к rollback.

### Migration tests

Проверить:

- таблица отсутствует — успешный no-op;
- валидный tombstone удаляет принадлежащий item;
- уже отсутствующий item не ломает миграцию;
- чужой tombstone не удаляет item;
- дубликаты безопасны;
- таблица удаляется после успешного commit;
- при ошибке транзакция откатывается;
- статистика корректна.

### HTTP tests

Проверить:

- authentication для library endpoints;
- `DELETE /items/{id}`: `204`, `400`, безопасный `404`;
- feed delete: `204`, безопасный `404`, cascade observable через последующие reads;
- `/add_deleted_items`: unauthenticated `401`, authenticated JSON `410`;
- сохранение текущих контрактов остальных endpoints;
- пустые массивы сериализуются как `[]`.

Все существующие тесты должны продолжить проходить после адаптации к новым пакетам и DI.

## Документация

Обновить:

- README: новый item delete, deprecated hide endpoint, необратимая startup-миграция и обязательный backup;
- Swagger/OpenAPI annotations и сгенерированные файлы;
- комментарии/TODO о временном legacy auth wiring;
- follow-up на окончательное удаление `/add_deleted_items` после проверки клиентов.

## Критерии готовности

- Ни один library handler не импортирует `internal/db` или `database/sql`.
- Ownership, not-found и pagination semantics доступны только через `library.Service`/repositories.
- SQLite library adapter использует внедрённый общий `*sql.DB` и context-aware API.
- Feed delete и item delete соответствуют согласованной атомарной и безопасной семантике.
- `deleted_items` безопасно мигрируется и удаляется.
- Старый hide endpoint не изменяет данные и возвращает authenticated JSON `410`.
- Composition root явно собирает resolver, repositories, service и handler.
- Graceful shutdown закрывает HTTP server и DB pool.
- Unit, contract, migration и HTTP tests проходят.
- `go test ./...` и существующие project checks проходят.
