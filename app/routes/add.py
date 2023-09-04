from fastapi.responses import JSONResponse

from app.services.repositories.feed import FeedRepository
from app.serializers.feed import Feed


class _FeedInput(Feed):
    id: None = None


async def add_feed(feed: _FeedInput):
    item = await FeedRepository.create(feed)
    return JSONResponse(content={
        'created': True,
        'item': item.dict(),
    })
