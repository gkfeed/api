from fastapi.responses import JSONResponse
from pydantic import BaseModel

from app.services.repositories.feed import FeedRepository
from app.services.feed_factory import FeedFactory
from app.serializers.feed import Feed


class _FeedInput(Feed):
    id: None = None


class _LazyFeedInput(BaseModel):
    url: str


async def add_feed(feed: _FeedInput):
    item = await FeedRepository.create(feed)
    return JSONResponse(
        content={
            "created": True,
            "item": item.dict(),
        }
    )


async def add_feed_lazy(feed: _LazyFeedInput):
    item = await FeedFactory.create_from_url(feed.url)
    return JSONResponse(
        content={
            "created": True,
            "item": item.dict(),
        }
    )
