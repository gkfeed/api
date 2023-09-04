from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

_origins = ['*']


def setup(app: FastAPI):
    app.add_middleware(
        CORSMiddleware,
        allow_origins=_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
