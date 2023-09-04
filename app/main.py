from app import manage


app = manage.get_app()


if __name__ == '__main__':
    import uvicorn
    uvicorn.run(app)
