import json

from fastapi import Response, Request

from app.services.repositories.feed import FeedRepository


async def get_subscriptions_list(_: Request):
    feeds = await FeedRepository.get_all()

    items = [f.dict() for f in feeds]

    content = json.dumps(items)
    return Response(content=content, media_type="application/json")
