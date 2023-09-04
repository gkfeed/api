from app.models.item import Item as _Item
from app.serializers.feed import Feed, Item


class ItemsRepository:
    def __init__(self, feed: Feed):
        self.feed = feed

    async def get_all(self) -> list[Item]:
        return [
            self._serialize_item(i)
            for i in await _Item.filter(feed_id=self.feed.id)
        ]

    async def add_items_to_feed(self, items: list[Item]):
        for item in items:
            await _Item.get_or_create(
                feed_id=self.feed.id,
                title=item.title,
                text=item.text,
                date=item.date,
                link=item.link
            )

    def _serialize_item(self, model_item: _Item) -> Item:
        return Item(
            title=model_item.title,
            text=model_item.text,
            date=model_item.date,
            link=model_item.link
        )
