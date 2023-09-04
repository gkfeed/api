from fastapi.responses import JSONResponse

from app.services.repositories.feed import FeedRepository


async def delete_feed(id: int):
    item = await FeedRepository.get_by_id(id)
    await FeedRepository.delete_by_id(id)
    return JSONResponse(content={
        'deleted': True,
        'item': item.dict(),
    })
