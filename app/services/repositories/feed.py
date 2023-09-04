from app.models.feed import Feed as _Feed
from app.serializers.feed import Feed


# NOTE: SubsRepository?
class FeedRepository:
    @classmethod
    async def create(cls, item: Feed) -> Feed:
        item = await _Feed.create(title=item.title, url=item.url, type=item.type)
        return await cls._unserialize(item)

    @classmethod
    async def get_all(cls) -> list[Feed]:
        return [await cls._unserialize(f) for f in (await _Feed.all())]

    @classmethod
    async def get_by_id(cls, id: int) -> Feed:
        if not (item := await _Feed.filter(id=id).first()):
            raise ValueError(f'No feed found by id: {id}')

        return await cls._unserialize(item)

    @classmethod
    async def delete_by_id(cls, id: int) -> None:
        if not (item := await _Feed.filter(id=id).first()):
            raise ValueError(f'No feed found by id: {id}')

        await _Feed.delete(item)

    @classmethod
    async def _unserialize(cls, feed: _Feed) -> Feed:
        return Feed(id=feed.id, title=feed.title, url=feed.url, type=feed.type)
